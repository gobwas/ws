package wsutil

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"

	"github.com/gobwas/pool/pbufio"
	"github.com/gobwas/ws"
)

// DebugDialer is a wrapper around ws.Dialer. It tracks i/o of WebSocket
// handshake. That is, it gives ability to receive copied HTTP request and
// response bytes that made inside Dialer.Dial().
//
// Note that it must not be used in production applications that requires
// Dial() efficiency.
type DebugDialer struct {
	// Dialer points to a Dialer that will make Dial(). If Dialer is nil then
	// the empty Dialer will be used.
	Dialer *ws.Dialer

	// OnRequest and OnResponse are the callbacks that will be called with the
	// HTTP request and response respectively.
	OnRequest, OnResponse func([]byte)
}

// Dial connects to the url host and upgrades connection to WebSocket. It makes
// it by calling d.Dialer.Dial().
func (d *DebugDialer) Dial(ctx context.Context, urlstr string) (conn net.Conn, br *bufio.Reader, hs ws.Handshake, err error) {
	// Need to copy Dialer to prevent original object mutation.
	var dialer ws.Dialer
	if d.Dialer == nil {
		dialer = ws.Dialer{}
	} else {
		dialer = *d.Dialer
	}
	var (
		reqBuf bytes.Buffer
		resBuf bytes.Buffer
	)
	userWrap := dialer.WrapConn
	dialer.WrapConn = func(c net.Conn) net.Conn {
		if userWrap != nil {
			c = userWrap(c)
		}
		conn = c
		return rwConn{conn,
			io.TeeReader(conn, &resBuf),
			io.MultiWriter(conn, &reqBuf),
		}
	}

	_, br, hs, err = dialer.Dial(ctx, urlstr)
	if err != nil {
		return
	}

	if onRequest := d.OnRequest; onRequest != nil {
		onRequest(reqBuf.Bytes())
	}
	if onResponse := d.OnResponse; onResponse != nil {
		if br == nil {
			onResponse(resBuf.Bytes())
		} else {
			p := resBuf.Bytes()
			m := len(p) - br.Buffered()
			res := p[:m]
			rem := p[m:]

			// Release reader.
			ws.PutReader(br)

			br = pbufio.GetReader(
				io.MultiReader(
					bytes.NewReader(rem),
					conn,
				),
				len(p),
			)

			onResponse(res)
		}
	}

	return conn, br, hs, nil
}

type rwConn struct {
	net.Conn

	r io.Reader
	w io.Writer
}

func (rwc rwConn) Read(p []byte) (int, error) {
	return rwc.r.Read(p)
}
func (rwc rwConn) Write(p []byte) (int, error) {
	return rwc.w.Write(p)
}
