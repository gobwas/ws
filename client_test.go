package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestDialerHandshake(t *testing.T) {
	for i, test := range []struct {
		res       *http.Response
		accept    bool
		protocols []string
		err       error
	}{
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: true,
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:  []string{"Upgrade"},
					headerUpgrade:     []string{"websocket"},
					headerSecProtocol: []string{"json"},
				},
			},
			protocols: []string{"xml", "json", "soap"},
			accept:    true,
		},
		{
			res: &http.Response{
				StatusCode: 400,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			err: ErrBadStatus,
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Error"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			err: ErrBadConnection,
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"iproto"},
				},
			},
			err: ErrBadUpgrade,
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: false,
			err:    ErrBadSecAccept,
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:  []string{"Upgrade"},
					headerUpgrade:     []string{"websocket"},
					headerSecProtocol: []string{"oops"},
				},
			},
			accept: true,
			err:    ErrBadSubProtocol,
		},
	} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			rb := &bytes.Buffer{}
			wb := &bytes.Buffer{}
			wbuf := bufio.NewReader(wb)

			sig := make(chan struct{})
			go func() {
				<-sig
				req, err := http.ReadRequest(wbuf)
				if err != nil {
					t.Fatal(err)
				}
				var nonce string
				if test.accept {
					nonce = req.Header.Get(headerSecKey)
				} else {
					k := make([]byte, nonceSize)
					rand.Read(k)
					nonce = string(k)
				}

				accept := makeAccept(strToNonce(nonce))
				test.res.Header.Set(headerSecAccept, accept)
				test.res.Request = req
				test.res.Write(rb)

				sig <- struct{}{}
			}()

			conn := &stubConn{
				read: func(p []byte) (int, error) {
					<-sig
					return rb.Read(p)
				},
				write: func(p []byte) (int, error) {
					n, err := wb.Write(p)
					sig <- struct{}{}
					return n, err
				},
				close: func() error { return nil },
			}

			pr := stubReadPool{}
			pw := stubWritePool{}

			d := Dialer{
				Protocol: test.protocols,
				NetDial: func(_ context.Context, _, _ string) (net.Conn, error) {
					return conn, nil
				},
				ReaderPool: &pr,
				WriterPool: &pw,
			}

			_, _, err := d.Dial(context.Background(), "ws://gobwas.com", nil)
			if test.err != err {
				t.Fatalf("unexpected error: %v;\n\twant %v", err, test.err)
			}
		})
	}
}

func TestDialerCancelation(t *testing.T) {
	var (
		ioErrDeadlineExceeded = fmt.Errorf("stub deadline exceeded")
	)
	for _, test := range []struct {
		name           string
		dialDelay      time.Duration
		ctxTimeout     time.Duration
		ctxCancelAfter time.Duration
		err            error
	}{
		{
			ctxTimeout: time.Millisecond * 100,
			err:        ioErrDeadlineExceeded,
		},
		{
			ctxCancelAfter: time.Millisecond * 100,
			err:            ioErrDeadlineExceeded,
		},
		{
			ctxTimeout: time.Millisecond * 100,
			dialDelay:  time.Millisecond * 200,
			err:        context.DeadlineExceeded,
		},
		{
			ctxCancelAfter: time.Millisecond * 100,
			dialDelay:      time.Millisecond * 200,
			err:            context.Canceled,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var ts []*time.Timer
			deadline := make(chan error, 10)
			conn := &stubConn{
				setDeadline: func(t time.Time) error {
					if t.IsZero() {
						for _, t := range ts {
							t.Stop()
						}
						return nil
					}

					d := t.Sub(time.Now())
					if d < 0 {
						deadline <- ioErrDeadlineExceeded
					} else {
						ts = append(ts, time.AfterFunc(d, func() {
							deadline <- ioErrDeadlineExceeded
						}))
					}

					return nil
				},
				read: func(p []byte) (int, error) {
					if err := <-deadline; err != nil {
						return 0, err
					}
					return len(p), nil
				},
				write: func(p []byte) (int, error) {
					if err := <-deadline; err != nil {
						return 0, err
					}
					return len(p), nil
				},
			}

			d := Dialer{
				NetDial: func(ctx context.Context, _, _ string) (net.Conn, error) {
					if t := test.dialDelay; t != 0 {
						delay := time.After(t)
						select {
						case <-delay:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
					return conn, nil
				},
			}

			ctx := context.Background()
			if t := test.ctxTimeout; t != 0 {
				ctx, _ = context.WithTimeout(ctx, t)
			}
			if t := test.ctxCancelAfter; t != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				time.AfterFunc(t, cancel)
			}

			_, _, err := d.Dial(ctx, "ws://gobwas.com", nil)
			if err != test.err {
				t.Fatalf("unexpected error: %v; want %v", err, test.err)
			}
		})
	}
}

func TestHostPortResolve(t *testing.T) {
	for _, test := range []struct {
		url *url.URL
		ret string
	}{
		{
			url: &url.URL{Host: "example.com", Scheme: "ws"},
			ret: "example.com:80",
		},
		{
			url: &url.URL{Host: "example.com", Scheme: "wss"},
			ret: "example.com:443",
		},
		{
			url: &url.URL{Host: "example.com:3000", Scheme: "wss"},
			ret: "example.com:3000",
		},
	} {
		t.Run(test.url.String(), func(t *testing.T) {
			ret := hostport(test.url)
			if test.ret != ret {
				t.Errorf("expected %s; got %s", test.ret, ret)
			}
		})
	}
}

type stubConn struct {
	read             func([]byte) (int, error)
	write            func([]byte) (int, error)
	close            func() error
	setDeadline      func(time.Time) error
	setWriteDeadline func(time.Time) error
	setReadDeadline  func(time.Time) error
}

func (s stubConn) Read(p []byte) (int, error)  { return s.read(p) }
func (s stubConn) Write(p []byte) (int, error) { return s.write(p) }
func (s stubConn) Close() error                { return s.close() }
func (s stubConn) LocalAddr() net.Addr         { return nil }
func (s stubConn) RemoteAddr() net.Addr        { return nil }
func (s stubConn) SetDeadline(t time.Time) error {
	if s.setDeadline != nil {
		return s.setDeadline(t)
	}
	return nil
}
func (s stubConn) SetReadDeadline(t time.Time) error {
	if s.setReadDeadline != nil {
		return s.setReadDeadline(t)
	}
	return nil
}
func (s stubConn) SetWriteDeadline(t time.Time) error {
	if s.setWriteDeadline != nil {
		return s.setWriteDeadline(t)
	}
	return nil
}

func makeNonceFrom(bts []byte) (ret [nonceSize]byte) {
	base64.StdEncoding.Encode(ret[:], bts)
	return
}

func nonceAsSlice(bts [nonceSize]byte) []byte {
	return bts[:]
}

type stubPool struct {
	getCalls int
	putCalls int
}

type stubWritePool struct {
	stubPool
}

func (s *stubWritePool) Get(w io.Writer) *bufio.Writer { s.getCalls++; return bufio.NewWriter(w) }
func (s *stubWritePool) Put(bw *bufio.Writer)          { s.putCalls++ }

type stubReadPool struct {
	stubPool
}

func (s *stubReadPool) Get(r io.Reader) *bufio.Reader { s.getCalls++; return bufio.NewReader(r) }
func (s *stubReadPool) Put(br *bufio.Reader)          { s.putCalls++ }
