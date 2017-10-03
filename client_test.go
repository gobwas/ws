package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gobwas/httphead"
)

func TestDialerRequest(t *testing.T) {
	for _, test := range []struct {
		dialer Dialer
		url    string
		exp    *http.Request
		err    bool
	}{
		{
			url: "wss://example.org/chat",
			exp: setHttpProto(1, 1,
				mustMakeRequest("GET", "wss://example.org/chat", http.Header{
					headerUpgrade:    []string{"websocket"},
					headerConnection: []string{"Upgrade"},
					headerSecVersion: []string{"13"},
					headerSecKey:     []string{"some key"},
				}),
			),
		},
		{
			dialer: Dialer{
				Protocols: []string{"foo", "bar"},
				Extensions: []httphead.Option{
					httphead.NewOption("foo", map[string]string{
						"bar": "1",
					}),
					httphead.NewOption("baz", nil),
				},
				Header: func(w io.Writer) {
					http.Header{
						"Origin": []string{"who knows"},
					}.Write(w)
				},
			},
			url: "wss://example.org/chat",
			exp: setHttpProto(1, 1,
				mustMakeRequest("GET", "wss://example.org/chat", http.Header{
					headerUpgrade:    []string{"websocket"},
					headerConnection: []string{"Upgrade"},
					headerSecVersion: []string{"13"},
					headerSecKey:     []string{"some key"},

					headerSecProtocol:   []string{"foo, bar"},
					headerSecExtensions: []string{"foo;bar=1,baz"},

					headerOrigin: []string{"who knows"},
				}),
			),
		},
	} {
		t.Run("", func(t *testing.T) {
			u, err := url.ParseRequestURI(test.url)
			if err != nil {
				t.Fatal(err)
			}

			var ErrStub = errors.New("stub")
			var buf bytes.Buffer
			conn := stubConn{
				read: func(p []byte) (int, error) {
					return 0, ErrStub
				},
				write: func(p []byte) (int, error) {
					return buf.Write(p)
				},
			}

			_, _, err = test.dialer.request(context.Background(), &conn, u)
			if err == ErrStub {
				err = nil
			}
			if test.err && err == nil {
				t.Errorf("expected error; got nil")
			}
			if !test.err && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.err {
				return
			}

			act := buf.Bytes()
			exp := dumpRequest(test.exp)

			act = sortHeaders(maskHeader(act, headerSecKey, "<masked>"))
			exp = sortHeaders(maskHeader(exp, headerSecKey, "<masked>"))

			if !bytes.Equal(act, exp) {
				t.Errorf("unexpected request:\nact:\n%s\nexp:\n%s\n", act, exp)
			}
			if _, err := http.ReadRequest(bufio.NewReader(&buf)); err != nil {
				t.Errorf("read request error: %s", err)
				return
			}
		})
	}
}

func makeAccept(nonce [nonceSize]byte) []byte {
	accept := make([]byte, acceptSize)
	putAccept(nonce, accept)
	return accept
}

func BenchmarkPutAccept(b *testing.B) {
	var nonce [nonceSize]byte
	_, err := rand.Read(nonce[:])
	if err != nil {
		b.Fatal(err)
	}
	p := make([]byte, acceptSize)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		putAccept(nonce, p)
	}
}

func BenchmarkCheckNonce(b *testing.B) {
	var nonce [nonceSize]byte
	_, err := rand.Read(nonce[:])
	if err != nil {
		b.Fatal(err)
	}

	accept := makeAccept(nonce)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_ = checkNonce(accept, nonce)
	}
}

func TestDialerHandshake(t *testing.T) {
	for _, test := range []struct {
		name   string
		dialer Dialer
		res    *http.Response
		frames []Frame
		accept bool
		err    error
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
			dialer: Dialer{
				Protocols: []string{"xml", "json", "soap"},
			},
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
			accept: true,
		},
		{
			dialer: Dialer{
				Protocols: []string{"xml", "json", "soap"},
			},
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
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: true,
			frames: []Frame{
				NewTextFrame("hello, gopherizer!"),
			},
		},

		// Error cases.

		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
				},
			},
			accept: true,
			err:    ErrBadUpgrade,
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
			err: statusError{400, http.StatusText(400)},
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"oops!"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: true,
			err:    ErrBadConnection,
		},
		{
			res: &http.Response{
				StatusCode: 101,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"oops!"},
				},
			},
			accept: true,
			err:    ErrBadUpgrade,
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
					headerSecProtocol: []string{"oops!"},
				},
			},
			accept: true,
			err:    ErrBadSubProtocol,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rb := &bytes.Buffer{}
			wb := &bytes.Buffer{}
			wbuf := bufio.NewReader(wb)

			sig := make(chan struct{})
			go func() {
				// This routine is our fake web-server. It reads request after
				// client wrote it. Then it optinally could send some frames
				// set in test case.
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
				test.res.Header.Set(headerSecAccept, string(accept))
				test.res.Request = req
				test.res.Write(rb)

				for _, f := range test.frames {
					if err := WriteFrame(rb, f); err != nil {
						t.Fatal(err)
					}
				}

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

			test.dialer.NetDial = func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			}

			_, br, _, err := test.dialer.Dial(context.Background(), "ws://gobwas.com")
			if test.err != err {
				t.Fatalf("unexpected error: %v;\n\twant %v", err, test.err)
			}

			if len(test.frames) > 0 && br == nil {
				t.Fatalf("can not read frames because Dial() returned empty bufio.Reader")
			}
			for i, exp := range test.frames {
				act, err := ReadFrame(br)
				if err != nil {
					t.Fatalf("can not read %d-th frame: %v", i, err)
				}
				if act.Header != exp.Header {
					t.Fatalf(
						"unexpected %d-th frame header: %v; want %v",
						i, act.Header, exp.Header,
					)
				}
				if !bytes.Equal(act.Payload, exp.Payload) {
					t.Fatalf(
						"unexpected %d-th frame payload:\n%v\nwant:\n%v",
						i, act.Payload, exp.Payload,
					)
				}
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

			_, _, _, err := d.Dial(ctx, "ws://gobwas.com")
			if err != test.err {
				t.Fatalf("unexpected error: %v; want %v", err, test.err)
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
