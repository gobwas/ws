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

type DebugDialer struct {
	Dialer     *ws.Dialer
	OnRequest  func([]byte, *http.Request)
	OnResponse func([]byte, *http.Response)
}

func (d *DebugDialer) Dial(ctx context.Context, urlstr string) (conn net.Conn, br *bufio.Reader, hs ws.Handshake, err error) {
	var dialer ws.Dialer
	if d.Dialer == nil {
		dialer = ws.Dialer{}
	} else {
		dialer = *d.Dialer
	}
	var (
		rawConn net.Conn
		reqBuf  bytes.Buffer

		req  *http.Request
		reqp []byte

		res  *http.Response
		resp []byte
	)
	userWrap := dialer.WrapConn
	dialer.WrapConn = func(conn net.Conn) net.Conn {
		if userWrap != nil {
			conn = userWrap(conn)
		}
		rawConn = conn
		br = pbufio.GetReader(rawConn, 4096)

		return rwConn{
			rawConn,
			&responseLimitedReader{r: br, res: &res, resp: &resp},
			io.MultiWriter(rawConn, &reqBuf),
		}
	}

	_, _, hs, err = dialer.Dial(ctx, urlstr)
	if br.Buffered() == 0 || err != nil {
		pbufio.PutReader(br)
		br = nil
	}
	if err != nil {
		return
	}

	reqp = reqBuf.Bytes()
	reqbr := pbufio.GetReader(&reqBuf, 4096)
	defer pbufio.PutReader(reqbr)
	if req, err = http.ReadRequest(reqbr); err != nil {
		return
	}
	if onRequest := d.OnRequest; onRequest != nil {
		onRequest(reqp, req)
	}
	if onResponse := d.OnResponse; onResponse != nil {
		onResponse(resp, res)
	}

	return rawConn, br, hs, nil
}

type responseLimitedReader struct {
	r   io.Reader
	b   io.Reader
	n   int
	err error

	res  **http.Response
	resp *[]byte
}

func (r *responseLimitedReader) Read(p []byte) (n int, err error) {
	if r.b == nil {
		buf := bytes.Buffer{}
		tee := io.TeeReader(r.r, &buf)
		*r.res, r.err = http.ReadResponse(bufio.NewReader(tee), nil)

		bts := buf.Bytes()
		end := bytes.Index(bts, []byte("\r\n\r\n"))
		end += int((*r.res).ContentLength)
		end += 4
		r.b = bytes.NewReader(bts[:end])

		*r.resp = bts[:end]
	}
	if r.err != nil {
		return 0, r.err
	}
	return r.b.Read(p)
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
