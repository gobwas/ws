package wsutil

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"

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
		br = pbufio.GetReader(conn, 4096)

		return rwConn{conn,
			// Limit read from conn inside Dialer.Dial() to read response only.
			// That is, responseLimitedReader reads whole response on first Read().
			// After that, it returns EOF when response bytes are all read, even if
			// there present more bytes in buffer br.
			//
			// Note that this will lead Dial() to always return nil buffer.
			// This will allow us to return conn and br without checking unread
			// bytes from Dial()'s buffer.
			&responseLimitedReader{
				Source: br,
				Buffer: &resBuf,
			},
			// Write end is much simplier – it just duplicates output stream
			// into the request buffer.
			io.MultiWriter(conn, &reqBuf),
		}
	}

	_, _, hs, err = dialer.Dial(ctx, urlstr)
	if br.Buffered() == 0 || err != nil {
		// No payload left in the buffer.
		pbufio.PutReader(br)
		br = nil
	}
	if err != nil {
		return
	}

	if onRequest := d.OnRequest; onRequest != nil {
		onRequest(reqBuf.Bytes())
	}
	if onResponse := d.OnResponse; onResponse != nil {
		onResponse(resBuf.Bytes())
	}

	return conn, br, hs, nil
}

type responseLimitedReader struct {
	Source io.Reader
	Buffer *bytes.Buffer

	resp io.Reader
	err  error
}

var headersEnd = []byte("\r\n\r\n")

func (r *responseLimitedReader) Read(p []byte) (n int, err error) {
	if r.resp == nil {
		var resp *http.Response
		tee := io.TeeReader(r.Source, r.Buffer)
		resp, r.err = http.ReadResponse(bufio.NewReader(tee), nil)

		bts := r.Buffer.Bytes()
		end := bytes.Index(bts, headersEnd)
		end += len(headersEnd)
		end += int(resp.ContentLength)

		r.resp = bytes.NewReader(bts[:end])
	}
	if r.err != nil {
		return 0, r.err
	}
	return r.resp.Read(p)
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
