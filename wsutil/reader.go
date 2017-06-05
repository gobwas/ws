package wsutil

import (
	"io"
	"io/ioutil"
	"strconv"

	"github.com/gobwas/pool/pbytes"
	"github.com/gobwas/ws"
)

type FrameHandler func(h ws.Header, r io.Reader) error

func PingHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, rd io.Reader) (err error) {
		var p []byte
		if h.Length != 0 {
			// int(h.Length) is safe here because control frame could be < 125
			// bytes length by RFC.
			p = pbytes.GetBufLen(int(h.Length))
			defer pbytes.PutBuf(p)

			_, err = io.ReadFull(rd, p)
			if err != nil {
				return
			}
		}

		f := ws.NewPongFrame(p)
		if state.Is(ws.StateClientSide) {
			f = ws.MaskFrameInplace(f)
		}

		return ws.WriteFrame(w, f)
	}
}

func PongHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, rd io.Reader) (err error) {
		if h.Length == 0 {
			return nil
		}

		// int(h.Length) is safe here because control frame could be < 125
		// bytes length by RFC.
		buf := pbytes.GetBufLen(int(h.Length))
		defer pbytes.PutBuf(buf)

		// Discard pong message according to the RFC6455:
		// A Pong frame MAY be sent unsolicited. This serves as a
		// unidirectional heartbeat. A response to an unsolicited Pong frame
		// is not expected.
		_, err = io.CopyBuffer(ioutil.Discard, rd, buf)

		return
	}
}

func CloseHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, rd io.Reader) (err error) {
		var (
			f      ws.Frame
			code   ws.StatusCode
			reason string
		)
		if h.Length == 0 {
			f = ws.CloseFrame
			code = ws.StatusNoStatusRcvd
		} else {
			// int(h.Length) is safe here because control frame could be < 125
			// bytes length by RFC.
			p := pbytes.GetBufLen(int(h.Length))
			defer pbytes.PutBuf(p)

			_, err = io.ReadFull(rd, p)
			if err != nil {
				return
			}

			code, reason = ws.ParseCloseFrameData(p)

			if e := ws.CheckCloseFrameData(code, reason); e != nil {
				f = ws.NewCloseFrame(ws.StatusProtocolError, e.Error())
			} else {
				// RFC6455#5.5.1:
				// If an endpoint receives a Close frame and did not previously
				// send a Close frame, the endpoint MUST send a Close frame in
				// response. (When sending a Close frame in response, the endpoint
				// typically echos the status code it received.)
				f = ws.NewCloseFrame(code, "")
			}
		}

		if state.Is(ws.StateClientSide) {
			f = ws.MaskFrameInplace(f)
		}

		if err = ws.WriteFrame(w, f); err == nil {
			err = ClosedError{code, reason}
		}

		return
	}
}

func ControlHandler(w io.Writer, state ws.State) FrameHandler {
	pingHandler := PingHandler(w, state)
	pongHandler := PongHandler(w, state)
	closeHandler := CloseHandler(w, state)

	return func(h ws.Header, rd io.Reader) (err error) {
		switch h.OpCode {
		case ws.OpPing:
			return pingHandler(h, rd)
		case ws.OpPong:
			return pongHandler(h, rd)
		case ws.OpClose:
			return closeHandler(h, rd)
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

type ClosedError struct {
	code   ws.StatusCode
	reason string
}

func (err ClosedError) Error() string {
	return "ws closed: " + strconv.FormatUint(uint64(err.code), 10) + " " + err.reason
}

func (err ClosedError) Reason() string {
	return err.reason
}

func (err ClosedError) Code() ws.StatusCode {
	return err.code
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
	if h.Masked {
		rd = NewCipherReader(rd, h.Mask)
	}

	if r.state.Is(ws.StateFragmented) && h.OpCode.IsControl() {
		if hi := r.handler.intermediate; hi != nil {
			err = hi(h, rd)
		}
		if err == nil {
			// Ensure that src is empty.
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
