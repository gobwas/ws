package wsutil

import (
	"bytes"
	"io"
	"testing"

	"github.com/gobwas/ws"
)

func TestReadMessageEOF(t *testing.T) {
	for _, test := range []struct {
		source func() io.Reader
		err    error
	}{
		{
			source: func() io.Reader { return eofReader },
			err:    io.EOF,
		},
		{
			source: func() io.Reader {
				// This case tests that ReadMessage still fails after
				// successfully reading header bytes frame via ws.ReadHeader()
				// and non-successfully read of the body.
				var buf bytes.Buffer
				f := ws.NewTextFrame("this part will be lost")
				if err := ws.WriteHeader(&buf, f.Header); err != nil {
					panic(err)
				}
				return &buf
			},
			err: io.ErrUnexpectedEOF,
		},
	} {
		t.Run("", func(t *testing.T) {
			ms, err := ReadMessage(test.source(), 0, nil)
			if n := len(ms); n > 0 {
				t.Errorf("unexpected number of read messages: %d; want %d", n, 0)
			}
			if err != test.err {
				t.Errorf("unexpected error: %v; want %v", err, test.err)
			}
		})
	}
}
