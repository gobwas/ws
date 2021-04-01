package wsutil

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/gobwas/ws"
)

func TestControlHandler(t *testing.T) {
	for _, test := range []struct {
		name  string
		state ws.State
		in    ws.Frame
		out   ws.Frame
		noOut bool
		err   error
	}{
		{
			name: "ping",
			in:   ws.NewPingFrame(nil),
			out:  ws.NewPongFrame(nil),
		},
		{
			name: "ping",
			in:   ws.NewPingFrame([]byte("catch the ball")),
			out:  ws.NewPongFrame([]byte("catch the ball")),
		},
		{
			name:  "ping",
			state: ws.StateServerSide,
			in:    ws.MaskFrame(ws.NewPingFrame([]byte("catch the ball"))),
			out:   ws.NewPongFrame([]byte("catch the ball")),
		},
		{
			name: "ping",
			in:   ws.NewPingFrame(bytes.Repeat([]byte{0xfe}, 125)),
			out:  ws.NewPongFrame(bytes.Repeat([]byte{0xfe}, 125)),
		},
		{
			name:  "pong",
			in:    ws.NewPongFrame(nil),
			noOut: true,
		},
		{
			name:  "pong",
			in:    ws.NewPongFrame([]byte("catched")),
			noOut: true,
		},
		{
			name: "close",
			in:   ws.NewCloseFrame(nil),
			out:  ws.NewCloseFrame(nil),
			err: ClosedError{
				Code: ws.StatusNoStatusRcvd,
			},
		},
		{
			name: "close",
			in: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "goodbye!",
			)),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "",
			)),
			err: ClosedError{
				Code:   ws.StatusGoingAway,
				Reason: "goodbye!",
			},
		},
		{
			name: "close",
			in: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "bye",
			)),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "",
			)),
			err: ClosedError{
				Code:   ws.StatusGoingAway,
				Reason: "bye",
			},
		},
		{
			name:  "close",
			state: ws.StateServerSide,
			in: ws.MaskFrame(ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "goodbye!",
			))),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "",
			)),
			err: ClosedError{
				Code:   ws.StatusGoingAway,
				Reason: "goodbye!",
			},
		},
		{
			name: "close",
			in: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusNormalClosure, string([]byte{0, 200}),
			)),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusProtocolError, ws.ErrProtocolInvalidUTF8.Error(),
			)),
			err: ws.ErrProtocolInvalidUTF8,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if err := recover(); err != nil {
					stack := make([]byte, 4096)
					n := runtime.Stack(stack, true)
					t.Fatalf(
						"panic recovered: %v\n%s",
						err, stack[:n],
					)
				}
			}()
			var (
				out = bytes.NewBuffer(nil)
				in  = bytes.NewReader(test.in.Payload)
			)
			c := ControlHandler{
				Src:   in,
				Dst:   out,
				State: test.state,
			}

			err := c.Handle(test.in.Header)
			if err != test.err {
				t.Errorf("unexpected error: %v; want %v", err, test.err)
			}

			if in.Len() != 0 {
				t.Errorf("handler did not drained the input")
			}

			act := out.Bytes()
			switch {
			case len(act) == 0 && test.noOut:
				return
			case len(act) == 0 && !test.noOut:
				t.Errorf("unexpected silence")
			case len(act) > 0 && test.noOut:
				t.Errorf("unexpected sent frame")
			default:
				exp := ws.MustCompileFrame(test.out)
				if !bytes.Equal(act, exp) {
					fa := ws.MustReadFrame(bytes.NewReader(act))
					fe := ws.MustReadFrame(bytes.NewReader(exp))
					t.Errorf(
						"unexpected sent frame:\n\tact: %+v\n\texp: %+v\nbytes:\n\tact: %v\n\texp: %v",
						fa, fe, act, exp,
					)
				}
			}
		})
	}
}
