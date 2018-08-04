package wsutil

import (
	"bytes"
	"testing"

	"github.com/gobwas/ws"
)

func TestHandlerHandleClose(t *testing.T) {
	for _, test := range []struct {
		name  string
		state ws.State
		in    ws.Frame
		out   ws.Frame
		err   error
	}{
		{
			in:  ws.NewCloseFrame(nil),
			out: ws.NewCloseFrame(nil),
			err: ClosedError{
				Code: ws.StatusNoStatusRcvd,
			},
		},
		{
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
			{
				act := out.Bytes()
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
