package wsutil

import (
	"io"
	"io/ioutil"

	"github.com/gobwas/ws"
)

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

	header ws.Header // Current frame header.
	frame  io.Reader // Used to as frame reader.
	raw    io.Reader // Used to discard frames without cipher.
	utf8   UTF8Reader
}

func NewReader(r io.Reader, s ws.State) *Reader {
	return &Reader{
		Source: r,
		State:  s,
	}
}

// Read implements io.Reader. It reads the next message payload into p. It
// takes care on fragmented messages.
//
// You could get the initial message header with Header() call. Note that it
// should be done after Read().
func (r *Reader) Read(p []byte) (n int, err error) {
	if r.frame == nil {
		// NextFrame set for us r.frame and r.raw with next frame io.Reader. It
		// also could change r.State fragmented bit.
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
		r.frame = nil
		r.raw = nil

		if r.State.Is(ws.StateFragmented) {
			err = nil
		} else if r.CheckUTF8 && r.header.OpCode == ws.OpText && !r.utf8.Valid() {
			err = ErrInvalidUtf8
		}
	}

	return
}

// Discard discards current message payload.
func (r *Reader) Discard() error {
	if !r.State.Is(ws.StateFragmented) && r.raw == nil {
		// Nothing to discard.
		return nil
	}
	for {
		if _, err := io.Copy(ioutil.Discard, r.raw); err != nil {
			return err
		}
		if !r.State.Is(ws.StateFragmented) {
			return nil
		}
		if _, err := r.NextFrame(); err != nil {
			return err
		}
	}
}

// Header returns last read message header. That is, it intended to be called
// right after Read() done, to get the meta info about read bytes. Next call to
// Read() will destroy previously saved Header value.
func (r *Reader) Header() ws.Header {
	return r.header
}

// NextFrame prepares r to read next message. It returns received frame header
// and non-nil error on failure.
//
// Note that next NextFrame() call should be done after whole message read with
// r.Read() or discard with r.Discard().
//
// If you do not need to check frame header, you could use Read() directly,
// that will take care on all things. Eventually, after read message bytes you
// could call r.Header() to get the received message header.
func (r *Reader) NextFrame() (hdr ws.Header, err error) {
	hdr, err = ws.ReadHeader(r.Source)
	if err != nil {
		return
	}
	if !r.SkipHeaderCheck {
		err = ws.CheckHeader(hdr, r.State)
		if err != nil {
			return
		}
	}
	if !r.State.Is(ws.StateFragmented) {
		// We got initial frame header (not continuation of previous) so we
		// could save its header for further Header() call.
		r.header = hdr
	}

	// Save raw io.Reader to use it on discarding frame without ciphering.
	raw := io.LimitReader(r.Source, hdr.Length)

	frame := raw
	if hdr.Masked {
		frame = NewCipherReader(frame, hdr.Mask)
	}

	if r.State.Is(ws.StateFragmented) && hdr.OpCode.IsControl() {
		if cb := r.OnIntermediate; cb != nil {
			err = cb(hdr, frame)
		}
		if err == nil {
			// Ensure that src is empty.
			_, err = io.Copy(ioutil.Discard, raw)
		}
		return
	}

	if r.CheckUTF8 && r.header.OpCode == ws.OpText {
		r.utf8.Source = frame
		frame = &r.utf8
	}
	r.frame = frame
	r.raw = raw

	if hdr.OpCode == ws.OpContinuation {
		if cb := r.OnContinuation; cb != nil {
			err = cb(hdr, frame)
		}
	}

	r.State = r.State.SetOrClearIf(!hdr.Fin, ws.StateFragmented)

	return
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
