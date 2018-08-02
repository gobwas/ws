package wsutil

import (
	"io"
	"io/ioutil"
	"strconv"

	"github.com/gobwas/pool/pbytes"
	"github.com/gobwas/ws"
)

// FrameHandler handles parsed frame header and its body represetned by
// io.Reader.
type FrameHandler func(h ws.Header, r io.Reader) error

// ClosedError returned when peer has closed the connection with appropriate
// code and a textual reason.
type ClosedError struct {
	Code   ws.StatusCode
	Reason string
}

// Error implements error interface.
func (err ClosedError) Error() string {
	return "ws closed: " + strconv.FormatUint(uint64(err.Code), 10) + " " + err.Reason
}

// PingHandler returns FrameHandler that handles ping frame and writes
// specification compatible response to the w.
func PingHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, r io.Reader) (err error) {
		if h.Length == 0 {
			// The most common case when ping is empty.
			return ws.WriteHeader(w, ws.Header{
				Fin:    true,
				OpCode: ws.OpPong,
				Masked: state.Is(ws.StateClientSide),
			})
		}
		if err = ws.CheckHeader(h, state); err != nil {
			sendProtocolErrorCloseFrame(w, state, err)
			return
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
		_, err = io.Copy(w, r)
		if err == nil {
			err = w.Flush()
		}

		return err
	}
}

// PongHandler returns FrameHandler that handles pong frame by discarding it.
func PongHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, r io.Reader) (err error) {
		if h.Length == 0 {
			return nil
		}
		if err = ws.CheckHeader(h, state); err != nil {
			sendProtocolErrorCloseFrame(w, state, err)
			return
		}

		// int(h.Length) is safe here because control frame could be < 125
		// bytes length by RFC.
		buf := pbytes.GetLen(int(h.Length))
		defer pbytes.Put(buf)

		// Discard pong message according to the RFC6455:
		// A Pong frame MAY be sent unsolicited. This serves as a
		// unidirectional heartbeat. A response to an unsolicited Pong frame
		// is not expected.
		_, err = io.CopyBuffer(ioutil.Discard, r, buf)

		return
	}
}

// CloseHandler returns FrameHandler that handles close frame, makes protocol
// validity checks and writes specification compatible response to the w.
func CloseHandler(w io.Writer, state ws.State) FrameHandler {
	return func(h ws.Header, r io.Reader) (err error) {
		if err = ws.CheckHeader(h, state); err != nil {
			sendProtocolErrorCloseFrame(w, state, err)
			return
		}
		var (
			f      ws.Frame
			code   ws.StatusCode
			reason string
		)
		if h.Length == 0 {
			// Respond with no close status code.
			// This is okay by RFC.
			f = ws.NewCloseFrame(nil)
			// Due to RFC, we should interpret the code as no status code
			// received:
			//   If this Close control frame contains no status code, _The WebSocket
			//   Connection Close Code_ is considered to be 1005.
			//
			// See https://tools.ietf.org/html/rfc6455#section-7.1.5
			code = ws.StatusNoStatusRcvd
		} else {
			p := pbytes.GetLen(int(h.Length))
			defer pbytes.Put(p)
			_, err = io.ReadFull(r, p)
			if err != nil {
				return
			}

			code, reason = ws.ParseCloseFrameData(p)
			if err = ws.CheckCloseFrameData(code, reason); err != nil {
				sendProtocolErrorCloseFrame(w, state, err)
				return
			}

			// RFC6455#5.5.1:
			// If an endpoint receives a Close frame and did not previously
			// send a Close frame, the endpoint MUST send a Close frame in
			// response. (When sending a Close frame in response, the endpoint
			// typically echos the status code it received.)
			f = ws.NewCloseFrame(p[:2])
		}
		if err = sendFrame(w, state, f); err == nil {
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

	return func(h ws.Header, r io.Reader) (err error) {
		switch h.OpCode {
		case ws.OpPing:
			return pingHandler(h, r)
		case ws.OpPong:
			return pongHandler(h, r)
		case ws.OpClose:
			return closeHandler(h, r)
		}
		return
	}
}

func sendProtocolErrorCloseFrame(w io.Writer, state ws.State, err error) error {
	f := ws.NewCloseFrame(ws.NewCloseFrameBody(
		ws.StatusProtocolError, err.Error(),
	))
	return sendFrame(w, state, f)
}

func sendFrame(w io.Writer, state ws.State, f ws.Frame) error {
	if state.Is(ws.StateClientSide) {
		f = ws.MaskFrameInPlace(f)
	}
	return ws.WriteFrame(w, f)
}
