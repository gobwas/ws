package wsutil

import (
	"io"
	"io/ioutil"
	"strconv"

	"github.com/gobwas/pool/pbytes"
	"github.com/gobwas/ws"
)

type FrameHandler func(h ws.Header, r io.Reader) error

func ControlHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, rd io.Reader) (err error) {
		// int(h.Length) is safe cause control frame could be < 125 bytes length.
		p := pbytes.GetBufLen(int(h.Length))
		defer pbytes.PutBuf(p)

		_, err = io.ReadFull(rd, p)
		if err != nil {
			return
		}

		var f ws.Frame

		switch h.OpCode {
		default:
			return
		case ws.OpPing:
			f = ws.NewPongFrame(p)
		case ws.OpClose:
			code, reason := ws.ParseCloseFrameDataUnsafe(p)
			if code.Empty() {
				code = ws.StatusNoStatusRcvd
				f = ws.CloseFrame
			} else if err = ws.CheckCloseFrameData(code, reason); err != nil {
				code = ws.StatusProtocolError
				reason = err.Error()
				f = ws.NewCloseFrame(code, reason)
			} else {
				// [RFC6455:5.5.1]:
				// If an endpoint receives a Close frame and did not previously
				// send a Close frame, the endpoint MUST send a Close frame in
				// response. (When sending a Close frame in response, the endpoint
				// typically echos the status code it received.)
				f = ws.NewCloseFrame(code, "")
			}
			err = ErrClosed{code, reason}
		}

		if state.Is(ws.StateClientSide) {
			f = ws.MaskFrame(f)
		}
		if ew := ws.WriteFrame(w, f); ew != nil {
			err = ew
		}

		return
	}
}

func NextReader(r io.Reader, s ws.State) (h ws.Header, rd *Reader, err error) {
	rd = NewReader(r, s)
	h, err = rd.Next()
	return
}

type handler struct {
	continuation FrameHandler
	intermediate FrameHandler
}

type Reader struct {
	src     io.Reader
	state   ws.State
	payload io.Reader
	handler handler
}

type ErrClosed struct {
	code   ws.StatusCode
	reason string
}

func (err ErrClosed) Error() string {
	return "ws closed: " + strconv.FormatUint(uint64(err.code), 10) + " " + err.reason
}

func NewReader(r io.Reader, s ws.State) *Reader {
	return &Reader{
		src:   r,
		state: s,
	}
}

func (r *Reader) HandleContinuation(h FrameHandler) { r.handler.continuation = h }
func (r *Reader) HandleIntermediate(h FrameHandler) { r.handler.intermediate = h }

func (r *Reader) Read(p []byte) (n int, err error) {
	if r.payload == nil {
		_, err = r.Next()
		if err != nil || r.payload == nil {
			return
		}
	}

	n, err = r.payload.Read(p)

	if err == io.EOF {
		r.payload = nil
		if r.state.Is(ws.StateFragmented) {
			err = nil
		}
	}

	return
}

func (r *Reader) Next() (h ws.Header, err error) {
	h, err = ws.ReadHeader(r.src)
	if err != nil {
		return
	}
	if err = ws.CheckHeader(h, r.state); err != nil {
		return
	}

	src := io.LimitReader(r.src, h.Length)
	rd := src
	if mask := h.Mask; mask != nil {
		rd = NewCipherReader(rd, mask)
	}

	if r.state.Is(ws.StateFragmented) && h.OpCode.IsControl() {
		if hi := r.handler.intermediate; hi != nil {
			err = hi(h, rd)
		}
		if err == nil {
			_, err = io.Copy(ioutil.Discard, src)
		}
		return
	}

	if h.OpCode == ws.OpContinuation {
		if hc := r.handler.continuation; hc != nil {
			err = hc(h, rd)
		}
	}

	r.state = r.state.SetOrClearIf(!h.Fin, ws.StateFragmented)
	r.payload = rd

	return
}
