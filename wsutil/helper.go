package wsutil

import (
	"bytes"
	"io"
	"sync/atomic"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/internal/saferio"
)

// Message represents a message from peer, that could be presented in one or
// more frames. That is, it contains payload of all message fragments and
// operation code of initial frame for this message.
type Message struct {
	OpCode  ws.OpCode
	Payload []byte
}

// defaultPreallocLimit is the default for SetPreallocLimit(): the same as
// io.Copy() chunk size.
const defaultPreallocLimit = 32 * 1024

// Settings of the Reader used by ReadMessage(), ReadData() and their
// shortcuts. They are accessed atomically, so that the setters below are safe
// to call concurrently with the helpers.
var (
	helperMaxFrameSize   int64
	helperMaxMessageSize int64
	helperCheckUTF8      int32 = 1
	helperPreallocLimit  int64 = defaultPreallocLimit
)

// SetMaxFrameSize sets the Reader.MaxFrameSize used by ReadMessage(),
// ReadData() and their shortcuts. Zero, the default, means no limit.
//
// It is safe to call concurrently with the helpers.
func SetMaxFrameSize(n int64) {
	atomic.StoreInt64(&helperMaxFrameSize, n)
}

// MaxFrameSize returns the value set with SetMaxFrameSize().
func MaxFrameSize() int64 {
	return atomic.LoadInt64(&helperMaxFrameSize)
}

// SetMaxMessageSize sets the Reader.MaxMessageSize used by ReadMessage(),
// ReadData() and their shortcuts: the total length of all fragments of a
// message. Zero, the default, means no limit.
//
// It is safe to call concurrently with the helpers.
func SetMaxMessageSize(n int64) {
	atomic.StoreInt64(&helperMaxMessageSize, n)
}

// MaxMessageSize returns the value set with SetMaxMessageSize().
func MaxMessageSize() int64 {
	return atomic.LoadInt64(&helperMaxMessageSize)
}

// SetCheckUTF8 sets the [Reader.CheckUTF8] used by [ReadMessage], [ReadData]
// and their shortcuts. It is true by default.
//
// It is safe to call concurrently with the helpers.
func SetCheckUTF8(v bool) {
	var x int32
	if v {
		x = 1
	}
	atomic.StoreInt32(&helperCheckUTF8, x)
}

// CheckUTF8 returns the value set with SetCheckUTF8().
func CheckUTF8() bool {
	return atomic.LoadInt32(&helperCheckUTF8) != 0
}

// SetPreallocLimit sets the maximum peer-declared payload length that
// ReadMessage(), ReadData() and their shortcuts trust for allocation before
// the payload is read; a payload declared bigger than that is read into a
// buffer that grows as bytes actually arrive. The default is 32 KiB, the
// io.Copy() chunk size. Limits set with SetMaxFrameSize() and
// SetMaxMessageSize() reject frames; they do not raise this one.
//
// It panics if n is not positive: trusting nothing is not a sane
// configuration, and the mistake is better reported here than in the middle of
// a read.
//
// It is safe to call concurrently with the helpers.
func SetPreallocLimit(n int64) {
	if n <= 0 {
		panic("wsutil: SetPreallocLimit() limit must be greater than zero")
	}
	atomic.StoreInt64(&helperPreallocLimit, n)
}

// PreallocLimit returns the value set with SetPreallocLimit().
func PreallocLimit() int64 {
	return atomic.LoadInt64(&helperPreallocLimit)
}

// DefaultReader returns a Reader for r and s configured with the package-level
// settings, that is, the Reader that ReadMessage(), ReadData() and their
// shortcuts read with. Unlike NewReader(), it applies MaxFrameSize() and
// MaxMessageSize() and CheckUTF8(). The OnIntermediate field is left for the
// caller to set.
func DefaultReader(r io.Reader, s ws.State) *Reader {
	return &Reader{
		Source:         r,
		State:          s,
		CheckUTF8:      CheckUTF8(),
		MaxFrameSize:   MaxFrameSize(),
		MaxMessageSize: MaxMessageSize(),
	}
}

// ReadMessage is a helper function that reads next message from r. It appends
// received message(s) to the third argument and returns the result of it and
// an error if some failure happened. That is, it probably could receive more
// than one message when peer sending fragmented message in multiple frames and
// want to send some control frame between fragments. Then returned slice will
// contain those control frames at first, and then result of gluing fragments.
//
// Note that there is no limit on the message size by default, since the
// message is read whole into memory; to set one, use SetMaxMessageSize().
// Allocation does not trust the peer-declared size beyond the limit set with
// SetPreallocLimit() and allocates as soon as the actual data is read.
// DefaultReader() returns the Reader these settings apply to.
func ReadMessage(r io.Reader, s ws.State, m []Message) ([]Message, error) {
	rd := DefaultReader(r, s)
	rd.OnIntermediate = func(hdr ws.Header, src io.Reader) error {
		bts, err := io.ReadAll(src)
		if err != nil {
			return err
		}
		m = append(m, Message{hdr.OpCode, bts})
		return nil
	}
	h, err := rd.NextFrame()
	if err != nil {
		return m, err
	}
	p, err := readMessagePayload(rd, h)
	if err != nil {
		return m, err
	}
	return append(m, Message{h.OpCode, p}), nil
}

// readMessagePayload reads from rd the payload of a message whose first frame
// header is hdr.
func readMessagePayload(rd *Reader, hdr ws.Header) ([]byte, error) {
	if hdr.Fin {
		// No more frames will be read, so the message size is known. Note that
		// it is declared by the peer and can not be trusted for allocation
		// beyond the prealloc limit, which saferio.ReadN() takes care of.
		//
		// MaxFrameSize and MaxMessageSize do not raise that trust: a bound
		// above the limit still allocates as bytes arrive, and a bound below
		// it has already rejected in NextFrame() any frame the limit could
		// matter for.
		return saferio.ReadN(rd, PreallocLimit(), hdr.Length)
	}
	// Message is fragmented, so its total size is unknown: let the buffer
	// grow as bytes actually arrive, like io.ReadAll() does. The first
	// fragment's declared length is a hint for the start size, trusted up to
	// the same limit as a whole frame would be.
	n := hdr.Length
	if lim := PreallocLimit(); n > lim {
		n = lim
	}
	var buf bytes.Buffer
	buf.Grow(int(n))
	_, err := buf.ReadFrom(rd)
	return buf.Bytes(), err
}

// ReadClientMessage reads next message from r, considering that caller
// represents server side.
// It is a shortcut for ReadMessage(r, ws.StateServerSide, m).
func ReadClientMessage(r io.Reader, m []Message) ([]Message, error) {
	return ReadMessage(r, ws.StateServerSide, m)
}

// ReadServerMessage reads next message from r, considering that caller
// represents client side.
// It is a shortcut for ReadMessage(r, ws.StateClientSide, m).
func ReadServerMessage(r io.Reader, m []Message) ([]Message, error) {
	return ReadMessage(r, ws.StateClientSide, m)
}

// ReadData is a helper function that reads next data (non-control) message
// from rw.
// It takes care on handling all control frames. It will write response on
// control frames to the write part of rw. It blocks until some data frame
// will be received.
//
// Note this may handle and write control frames into the writer part of a
// given io.ReadWriter.
//
// Note that there is no limit on the message size by default, since the
// message is read whole into memory; to set one, use SetMaxMessageSize().
// Allocation does not trust the peer-declared size beyond the limit set with
// SetPreallocLimit() and allocates as soon as the actual data is read.
// DefaultReader() returns the Reader these settings apply to.
func ReadData(rw io.ReadWriter, s ws.State) ([]byte, ws.OpCode, error) {
	return readData(rw, s, ws.OpText|ws.OpBinary)
}

// ReadClientData reads next data message from rw, considering that caller
// represents server side. It is a shortcut for ReadData(rw, ws.StateServerSide).
//
// Note this may handle and write control frames into the writer part of a
// given io.ReadWriter.
func ReadClientData(rw io.ReadWriter) ([]byte, ws.OpCode, error) {
	return ReadData(rw, ws.StateServerSide)
}

// ReadClientText reads next text message from rw, considering that caller
// represents server side. It is a shortcut for ReadData(rw, ws.StateServerSide).
// It discards received binary messages.
//
// Note this may handle and write control frames into the writer part of a
// given io.ReadWriter.
func ReadClientText(rw io.ReadWriter) ([]byte, error) {
	p, _, err := readData(rw, ws.StateServerSide, ws.OpText)
	return p, err
}

// ReadClientBinary reads next binary message from rw, considering that caller
// represents server side. It is a shortcut for ReadData(rw, ws.StateServerSide).
// It discards received text messages.
//
// Note this may handle and write control frames into the writer part of a given
// io.ReadWriter.
func ReadClientBinary(rw io.ReadWriter) ([]byte, error) {
	p, _, err := readData(rw, ws.StateServerSide, ws.OpBinary)
	return p, err
}

// ReadServerData reads next data message from rw, considering that caller
// represents client side. It is a shortcut for ReadData(rw, ws.StateClientSide).
//
// Note this may handle and write control frames into the writer part of a
// given io.ReadWriter.
func ReadServerData(rw io.ReadWriter) ([]byte, ws.OpCode, error) {
	return ReadData(rw, ws.StateClientSide)
}

// ReadServerText reads next text message from rw, considering that caller
// represents client side. It is a shortcut for ReadData(rw, ws.StateClientSide).
// It discards received binary messages.
//
// Note this may handle and write control frames into the writer part of a given
// io.ReadWriter.
func ReadServerText(rw io.ReadWriter) ([]byte, error) {
	p, _, err := readData(rw, ws.StateClientSide, ws.OpText)
	return p, err
}

// ReadServerBinary reads next binary message from rw, considering that caller
// represents client side. It is a shortcut for ReadData(rw, ws.StateClientSide).
// It discards received text messages.
//
// Note this may handle and write control frames into the writer part of a
// given io.ReadWriter.
func ReadServerBinary(rw io.ReadWriter) ([]byte, error) {
	p, _, err := readData(rw, ws.StateClientSide, ws.OpBinary)
	return p, err
}

// WriteMessage is a helper function that writes message to the w. It
// constructs single frame with given operation code and payload.
// It uses given state to prepare side-dependent things, like cipher
// payload bytes from client to server. It will not mutate p bytes if
// cipher must be made.
//
// If you want to write message in fragmented frames, use Writer instead.
func WriteMessage(w io.Writer, s ws.State, op ws.OpCode, p []byte) error {
	return writeFrame(w, s, op, true, p)
}

// WriteServerMessage writes message to w, considering that caller
// represents server side.
func WriteServerMessage(w io.Writer, op ws.OpCode, p []byte) error {
	return WriteMessage(w, ws.StateServerSide, op, p)
}

// WriteServerText is the same as WriteServerMessage with
// ws.OpText.
func WriteServerText(w io.Writer, p []byte) error {
	return WriteServerMessage(w, ws.OpText, p)
}

// WriteServerBinary is the same as WriteServerMessage with
// ws.OpBinary.
func WriteServerBinary(w io.Writer, p []byte) error {
	return WriteServerMessage(w, ws.OpBinary, p)
}

// WriteClientMessage writes message to w, considering that caller
// represents client side.
func WriteClientMessage(w io.Writer, op ws.OpCode, p []byte) error {
	return WriteMessage(w, ws.StateClientSide, op, p)
}

// WriteClientText is the same as WriteClientMessage with
// ws.OpText.
func WriteClientText(w io.Writer, p []byte) error {
	return WriteClientMessage(w, ws.OpText, p)
}

// WriteClientBinary is the same as WriteClientMessage with
// ws.OpBinary.
func WriteClientBinary(w io.Writer, p []byte) error {
	return WriteClientMessage(w, ws.OpBinary, p)
}

// HandleClientControlMessage handles control frame from conn and writes
// response when needed.
//
// It considers that caller represents server side.
func HandleClientControlMessage(conn io.Writer, msg Message) error {
	return HandleControlMessage(conn, ws.StateServerSide, msg)
}

// HandleServerControlMessage handles control frame from conn and writes
// response when needed.
//
// It considers that caller represents client side.
func HandleServerControlMessage(conn io.Writer, msg Message) error {
	return HandleControlMessage(conn, ws.StateClientSide, msg)
}

// HandleControlMessage handles message which was read by ReadMessage()
// functions.
//
// That is, it is expected, that payload is already unmasked and frame header
// were checked by ws.CheckHeader() call.
func HandleControlMessage(conn io.Writer, state ws.State, msg Message) error {
	return (ControlHandler{
		DisableSrcCiphering: true,
		Src:                 bytes.NewReader(msg.Payload),
		Dst:                 conn,
		State:               state,
	}).Handle(ws.Header{
		Length: int64(len(msg.Payload)),
		OpCode: msg.OpCode,
		Fin:    true,
		Masked: state.ServerSide(),
	})
}

// ControlFrameHandler returns FrameHandlerFunc for handling control frames.
// For more info see ControlHandler docs.
func ControlFrameHandler(w io.Writer, state ws.State) FrameHandlerFunc {
	return func(h ws.Header, r io.Reader) error {
		return (ControlHandler{
			DisableSrcCiphering: true,
			Src:                 r,
			Dst:                 w,
			State:               state,
		}).Handle(h)
	}
}

func readData(rw io.ReadWriter, s ws.State, want ws.OpCode) ([]byte, ws.OpCode, error) {
	controlHandler := ControlFrameHandler(rw, s)
	rd := DefaultReader(rw, s)
	rd.OnIntermediate = controlHandler
	for {
		hdr, err := rd.NextFrame()
		if err != nil {
			return nil, 0, err
		}
		if hdr.OpCode.IsControl() {
			if err := controlHandler(hdr, rd); err != nil {
				return nil, 0, err
			}
			continue
		}
		if hdr.OpCode&want == 0 {
			if err := rd.Discard(); err != nil {
				return nil, 0, err
			}
			continue
		}

		bts, err := readMessagePayload(rd, hdr)

		return bts, hdr.OpCode, err
	}
}
