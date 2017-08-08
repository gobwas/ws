package wsutil

import (
	"io"
	"io/ioutil"
	"strconv"

	"github.com/gobwas/pool/pbytes"
	"github.com/gobwas/ws"
)

type FrameHandler func(h ws.Header, r io.Reader) error

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

func PingHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, rd io.Reader) (err error) {
		if h.Length == 0 {
			// The most common case when ping is empty.
			return ws.WriteHeader(w, ws.Header{
				Fin:    true,
				OpCode: ws.OpPong,
				Masked: state.Is(ws.StateClientSide),
			})
		}

		// In other way reply with Pong frame with copied payload.
		// Note that int(h.Length) is safe here because control frame i
		// could be <= 125 bytes length by RFC.
		p := pbytes.GetLen(int(h.Length) + ws.HeaderSize(ws.Header{
			Length: h.Length,
			OpCode: ws.OpPong,
			Masked: state.Is(ws.StateClientSide),
		}))
		defer pbytes.Put(p)

		w := NewControlWriterBuffer(w, state, ws.OpPong, p)
		_, err = io.Copy(w, rd)
		if err == nil {
			err = w.Flush()
		}

		return err
	}
}

func PongHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, rd io.Reader) (err error) {
		if h.Length == 0 {
			return nil
		}

		// int(h.Length) is safe here because control frame could be < 125
		// bytes length by RFC.
		buf := pbytes.GetLen(int(h.Length))
		defer pbytes.Put(buf)

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
			p := pbytes.GetLen(int(h.Length))
			defer pbytes.Put(p)

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
			f = ws.MaskFrameInPlace(f)
		}

		if err = ws.WriteFrame(w, f); err == nil {
			err = ClosedError{code, reason}
		}

		return
	}
}

// ControlHandler return FrameHandler that handles control messages
// regarding to the given state and writes responses to w when
// needed.
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
