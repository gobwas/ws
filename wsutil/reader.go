package wsutil

import (
	"errors"
	"io"
	"io/ioutil"

	"github.com/gobwas/ws"
)

// ErrNoFrameAdvance means that Reader's Read() method was called without
// preceding NextFrame() call.
var ErrNoFrameAdvance = errors.New("no frame advance")

// Reader is a wrapper around source io.Reader which represents WebSocket
// connection. It contains options for reading messages from source.
//
// Reader implements io.Reader, which Read() method reads payload of incoming
// WebSocket frames. It also takes care on fragmented frames and possibly
// intermediate control frames between them.
//
// Note that Reader's methods are not goroutine safe.
type Reader struct {
	Source io.Reader
	State  ws.State

	// SkipHeaderCheck disables checking header bits to be RFC6455 compliant.
	SkipHeaderCheck bool

	// CheckUTF8 enables UTF-8 checks for text frames payload. If incoming
	// bytes are not valid UTF-8 sequence, ErrInvalidUTF8 returned.
	CheckUTF8 bool

	// TODO(gobwas): add max frame size limit here.

	OnContinuation FrameHandler
	OnIntermediate FrameHandler

	frame io.Reader        // Used to as frame reader.
	raw   io.LimitedReader // Used to discard frames without cipher.
	utf8  UTF8Reader       // Used to check UTF8 sequences if CheckUTF8 is true.
}

// NewReader creates new frame reader that reads from r keeping given state to
// make some protocol validity checks when it needed.
func NewReader(r io.Reader, s ws.State) *Reader {
	return &Reader{
		Source: r,
		State:  s,
	}
}

// NewClientSideReader is a helper function that calls NewReader with r and
// ws.StateClientSide.
func NewClientSideReader(r io.Reader) *Reader {
	return NewReader(r, ws.StateClientSide)
}

// NewServerSideReader is a helper function that calls NewReader with r and
// ws.StateServerSide.
func NewServerSideReader(r io.Reader) *Reader {
	return NewReader(r, ws.StateServerSide)
}

// Read implements io.Reader. It reads the next message payload into p.
// It takes care on fragmented messages.
//
// The error is io.EOF only if all of message bytes were read.
// If an io.EOF happens during reading some but not all the message bytes
// Read() returns io.ErrUnexpectedEOF.
//
// The error is ErrNoFrameAdvance if no NextFrame() call was made before
// reading next message bytes.
func (r *Reader) Read(p []byte) (n int, err error) {
	if r.frame == nil {
		if !r.State.Is(ws.StateFragmented) {
			// Every new Read() must be preceded by NextFrame() call.
			return 0, ErrNoFrameAdvance
		}
		// Read next continuation or intermediate control frame.
		_, err := r.NextFrame()
		if err != nil {
			return 0, err
		}
		if r.frame == nil {
			// We handled intermediate control and now got nothing to read.
			return 0, nil
		}
	}

	n, err = r.frame.Read(p)

	if err == io.EOF {
		switch {
		case r.raw.N != 0:
			err = io.ErrUnexpectedEOF

		case r.State.Is(ws.StateFragmented):
			err = nil
			r.resetFragment()

		case r.CheckUTF8 && r.utf8.Source != nil && !r.utf8.Valid():
			err = ErrInvalidUTF8

		default:
			r.reset()
		}
	}

	return
}

// Discard discards current message unread bytes.
// It discards all frames of fragmeneted message.
func (r *Reader) Discard() (err error) {
	for {
		_, err = io.Copy(ioutil.Discard, &r.raw)
		if err != nil {
			break
		}
		if !r.State.Is(ws.StateFragmented) {
			break
		}
		if _, err = r.NextFrame(); err != nil {
			break
		}
	}
	r.reset()
	return err
}

// NextFrame prepares r to read next message. It returns received frame header
// and non-nil error on failure.
//
// Note that next NextFrame() call must be done after receiving or discarding
// all current message bytes.
func (r *Reader) NextFrame() (hdr ws.Header, err error) {
	hdr, err = ws.ReadHeader(r.Source)
	if err != nil {
		if err == io.EOF && r.State.Is(ws.StateFragmented) {
			// If we are in fragmented state EOF means that is was totally
			// unexpected.
			//
			// NOTE: This is necessary to prevent callers such that
			// ioutil.ReadAll to receive some amount of bytes without an error.
			// ReadAll() ignores an io.EOF error, thus caller may think that
			// whole message fetched, but actually only part of it.
			err = io.ErrUnexpectedEOF
		}
		return
	}
	if !r.SkipHeaderCheck {
		if err = ws.CheckHeader(hdr, r.State); err != nil {
			return
		}
	}

	// Save raw reader to use it on discarding frame without ciphering and
	// other streaming checks.
	r.raw = io.LimitedReader{r.Source, hdr.Length}

	frame := io.Reader(&r.raw)
	if hdr.Masked {
		frame = NewCipherReader(frame, hdr.Mask)
	}
	if r.State.Is(ws.StateFragmented) && hdr.OpCode.IsControl() {
		if cb := r.OnIntermediate; cb != nil {
			err = cb(hdr, frame)
		}
		if err == nil {
			// Ensure that src is empty.
			_, err = io.Copy(ioutil.Discard, &r.raw)
		}
		return
	}
	if r.CheckUTF8 && hdr.OpCode == ws.OpText {
		r.utf8.Source = frame
		frame = &r.utf8
	}

	// Save reader with ciphering and other streaming checks.
	r.frame = frame

	if hdr.OpCode == ws.OpContinuation {
		if cb := r.OnContinuation; cb != nil {
			err = cb(hdr, frame)
		}
	}

	r.State = r.State.SetOrClearIf(!hdr.Fin, ws.StateFragmented)

	return
}

func (r *Reader) resetFragment() {
	r.raw = io.LimitedReader{}
	r.frame = nil
	// Reset source of UTF8Reader, not the state.
	r.utf8.Source = nil
}

func (r *Reader) reset() {
	r.raw = io.LimitedReader{}
	r.frame = nil
	r.utf8 = UTF8Reader{}
}

// NextReader prepares next message read from r. It returns header that
// describes the message and io.Reader to read message's payload. It returns
// non-nil error when it is not possible to read message's iniital frame.
//
// Note that next NextReader() on the same r should be done after reading all
// bytes from previously returned io.Reader. For more performant way to discard
// message use Reader and its Discard() method.
//
// Note that it will not handle any "intermediate" frames, that possibly could
// be received between text/binary continuation frames. That is, if peer sent
// text/binary frame with fin flag "false", then it could send ping frame, and
// eventually remaining part of text/binary frame with fin "true" – with
// NextReader() the ping frame will be dropped without any notice. To handle
// this rare, but possible situation (and if you do not know exactly which
// frames peer could send), you could use Reader with OnIntermediate field set.
func NextReader(r io.Reader, s ws.State) (ws.Header, io.Reader, error) {
	rd := &Reader{
		Source: r,
		State:  s,
	}
	header, err := rd.NextFrame()
	if err != nil {
		return header, nil, err
	}

	return header, rd, nil
}
