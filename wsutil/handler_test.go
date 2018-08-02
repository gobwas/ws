package wsutil

import (
	"bytes"
	"testing"

	"github.com/gobwas/ws"
)

func TestCloseHandler(t *testing.T) {
	for _, test := range []struct {
		name string
		in   ws.Frame
		out  ws.Frame
		err  error
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
				ws.StatusGoingAway, "bye bye!",
			)),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusGoingAway, "",
			)),
			err: ClosedError{
				Code:   ws.StatusGoingAway,
				Reason: "bye bye!",
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
		{
			in: ws.NewCloseFrame(make([]byte, ws.MaxControlFramePayloadSize+1)),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusProtocolError,
				ws.ErrProtocolControlPayloadOverflow.Error(),
			)),
			err: ws.ErrProtocolControlPayloadOverflow,
		},
		{
			in: ws.NewFrame(ws.OpClose, false, nil),
			out: ws.NewCloseFrame(ws.NewCloseFrameBody(
				ws.StatusProtocolError,
				ws.ErrProtocolControlNotFinal.Error(),
			)),
			err: ws.ErrProtocolControlNotFinal,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			h := CloseHandler(&out, 0)

			in := bytes.NewReader(test.in.Payload)
			err := h(test.in.Header, in)
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
