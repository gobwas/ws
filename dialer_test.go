package ws

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
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
			exp: setProto(1, 1,
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
				Header: HandshakeHeaderHTTP(http.Header{
					"Origin": []string{"who knows"},
				}),
			},
			url: "wss://example.org/chat",
			exp: setProto(1, 1,
				mustMakeRequest("GET", "wss://example.org/chat", http.Header{
					headerUpgrade:    []string{"websocket"},
					headerConnection: []string{"Upgrade"},
					headerSecVersion: []string{"13"},
					headerSecKey:     []string{"some key"},

					headerSecProtocol:   []string{"foo, bar"},
					headerSecExtensions: []string{"foo;bar=1,baz"},

					"Origin": []string{"who knows"},
				}),
			),
		},
	} {
		t.Run("", func(t *testing.T) {
			u, err := url.ParseRequestURI(test.url)
			if err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			conn := struct {
				io.Reader
				io.Writer
			}{io.LimitReader(&buf, 0), &buf}

			_, _, err = test.dialer.Upgrade(&conn, u)
			if err == io.EOF {
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

func makeAccept(nonce []byte) []byte {
	accept := make([]byte, acceptSize)
	initAcceptFromNonce(accept, nonce)
	return accept
}

func BenchmarkPutAccept(b *testing.B) {
	nonce := make([]byte, nonceSize)
	_, err := rand.Read(nonce)
	if err != nil {
		b.Fatal(err)
	}
	p := make([]byte, acceptSize)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		initAcceptFromNonce(p, nonce)
	}
}

func BenchmarkCheckNonce(b *testing.B) {
	nonce := make([]byte, nonceSize)
	_, err := rand.Read(nonce)
	if err != nil {
		b.Fatal(err)
	}

	accept := makeAccept(nonce)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = checkAcceptFromNonce(nonce, accept)
	}
}

func TestDialerHandshake(t *testing.T) {
	const (
		acceptNo = iota
		acceptInvalid
		acceptValid
	)
	for _, test := range []struct {
		name       string
		dialer     Dialer
		res        *http.Response
		frames     []Frame
		accept     int
		err        error
		wantBuffer bool
	}{
		{
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptValid,
		},
		{
			dialer: Dialer{
				Protocols: []string{"xml", "json", "soap"},
			},
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:  []string{"Upgrade"},
					headerUpgrade:     []string{"websocket"},
					headerSecProtocol: []string{"json"},
				},
			},
			accept: acceptValid,
		},
		{
			dialer: Dialer{
				Protocols: []string{"xml", "json", "soap"},
			},
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptValid,
		},
		{
			dialer: Dialer{
				Extensions: []httphead.Option{
					httphead.NewOption("foo", map[string]string{
						"bar": "1",
					}),
					httphead.NewOption("baz", nil),
				},
			},
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:    []string{"Upgrade"},
					headerUpgrade:       []string{"websocket"},
					headerSecExtensions: []string{"foo;bar=1"},
				},
			},
			accept: acceptValid,
		},
		{
			dialer: Dialer{
				Extensions: []httphead.Option{
					httphead.NewOption("foo", map[string]string{
						"bar": "1",
					}),
					httphead.NewOption("baz", nil),
				},
			},
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptValid,
		},
		{
			dialer: Dialer{
				Protocols: []string{"xml", "json", "soap"},
				Extensions: []httphead.Option{
					httphead.NewOption("foo", map[string]string{
						"bar": "1",
					}),
					httphead.NewOption("baz", nil),
				},
			},
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:    []string{"Upgrade"},
					headerUpgrade:       []string{"websocket"},
					headerSecProtocol:   []string{"json"},
					headerSecExtensions: []string{"foo;bar=1"},
				},
			},
			accept: acceptValid,
		},
		{
			name: "resp with frames",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptValid,
			frames: []Frame{
				NewTextFrame([]byte("hello, gopherizer!")),
			},
			wantBuffer: true,
		},
		{
			name: "resp with body",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
				Body:          ioutil.NopCloser(bytes.NewReader([]byte(`hello, gopher!`))),
				ContentLength: 14,
			},
			accept:     acceptValid,
			wantBuffer: true,
		},

		// Error cases.

		{
			name: "bad proto",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 2,
				ProtoMinor: 1,
				Header:     make(http.Header),
			},
			err: ErrHandshakeBadProtocol,
		},
		{
			name: "bad status",
			res: &http.Response{
				StatusCode: http.StatusBadRequest,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header:     make(http.Header),
			},
			err:        StatusError(400),
			wantBuffer: false,
		},
		{
			name: "bad status with body",
			res: &http.Response{
				StatusCode: http.StatusBadRequest,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header:     make(http.Header),
				Body: ioutil.NopCloser(bytes.NewReader(
					[]byte(`<error description here>`),
				)),
				ContentLength: 24,
			},
			err:        StatusError(400),
			wantBuffer: false,
		},
		{
			name: "bad upgrade",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
				},
			},
			accept: acceptValid,
			err:    ErrHandshakeBadUpgrade,
		},
		{
			name: "bad upgrade",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"oops"},
				},
			},
			accept: acceptValid,
			err:    ErrHandshakeBadUpgrade,
		},
		{
			name: "bad connection",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerUpgrade: []string{"websocket"},
				},
			},
			accept: acceptValid,
			err:    ErrHandshakeBadConnection,
		},
		{
			name: "bad connection",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"oops!"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptValid,
			err:    ErrHandshakeBadConnection,
		},
		{
			name: "bad accept",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptInvalid,
			err:    ErrHandshakeBadSecAccept,
		},
		{
			name: "bad accept",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection: []string{"Upgrade"},
					headerUpgrade:    []string{"websocket"},
				},
			},
			accept: acceptNo,
			err:    ErrHandshakeBadSecAccept,
		},
		{
			name: "bad subprotocol",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:  []string{"Upgrade"},
					headerUpgrade:     []string{"websocket"},
					headerSecProtocol: []string{"oops!"},
				},
			},
			accept: acceptValid,
			err:    ErrHandshakeBadSubProtocol,
		},
		{
			name: "bad extensions",
			res: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					headerConnection:    []string{"Upgrade"},
					headerUpgrade:       []string{"websocket"},
					headerSecExtensions: []string{"foo,bar;baz=1"},
				},
			},
			accept: acceptValid,
			err:    ErrHandshakeBadExtensions,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, server := net.Pipe()
			go func() {
				// This routine is our fake web-server. It reads request after
				// client wrote it. Then it optionally could send some frames
				// set in test case.
				req, err := http.ReadRequest(bufio.NewReader(client))
				if err != nil {
					panic(err)
				}

				switch test.accept {
				case acceptInvalid:
					k := make([]byte, nonceSize)
					rand.Read(k)
					nonce := string(k)
					accept := makeAccept(strToBytes(nonce))
					test.res.Header.Set(headerSecAccept, string(accept))
				case acceptValid:
					nonce := req.Header.Get(headerSecKey)
					accept := makeAccept(strToBytes(nonce))
					test.res.Header.Set(headerSecAccept, string(accept))
				}

				test.res.Request = req
				bts := dumpResponse(test.res)

				var buf bytes.Buffer
				for _, f := range test.frames {
					if err := WriteFrame(&buf, f); err != nil {
						panic(err)
					}
					bts = append(bts, buf.Bytes()...)
					buf.Reset()
				}

				client.Write(bts)
				client.Close()
			}()

			conn := &stubConn{
				read: func(p []byte) (int, error) {
					return server.Read(p)
				},
				write: func(p []byte) (int, error) {
					n, err := server.Write(p)
					return n, err
				},
				close: func() error { return nil },
			}

			test.dialer.NetDial = func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			}
			test.dialer.OnStatusError = func(status int, reason []byte, r io.Reader) {
				res, err := http.ReadResponse(
					bufio.NewReader(r),
					nil,
				)
				if err != nil {
					t.Errorf("read response inside OnStatusError error: %v", err)
				}
				if act, exp := dumpResponse(res), dumpResponse(test.res); !bytes.Equal(act, exp) {
					t.Errorf(
						"unexpected response from OnStatusError:\nact:\n%s\nexp:\n%s\n",
						act, exp,
					)
				}
			}

			_, br, _, err := test.dialer.Dial(context.Background(), "ws://gobwas.com")
			if test.err != err {
				t.Fatalf("unexpected error: %v;\n\twant %v", err, test.err)
			}

			if (test.wantBuffer || len(test.frames) > 0) && br == nil {
				t.Fatalf("Dial() returned empty bufio.Reader")
			}
			if !test.wantBuffer && br != nil {
				t.Fatalf("Dial() returned non-empty bufio.Reader")
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

// Used to emulate net.Error behavior, which is usually returned when
// connection deadline exceeds.
type errTimeout struct {
	error
}

func (errTimeout) Timeout() bool   { return true }
func (errTimeout) Temporary() bool { return false }

func TestDialerCancelation(t *testing.T) {
	ioErrDeadline := errTimeout{
		fmt.Errorf("stub: i/o timeout"),
	}

	for _, test := range []struct {
		name           string
		dialer         Dialer
		dialDelay      time.Duration
		ctxTimeout     time.Duration
		ctxCancelAfter time.Duration
		err            error
	}{
		{
			ctxTimeout: time.Millisecond * 100,
			err:        context.DeadlineExceeded,
		},
		{
			ctxCancelAfter: time.Millisecond * 100,
			err:            context.Canceled,
		},
		{
			dialer: Dialer{
				Timeout: time.Millisecond * 100,
			},
			ctxTimeout: time.Millisecond * 150,
			err:        context.DeadlineExceeded,
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
			var timer *time.Timer
			deadline := make(chan error, 10)
			conn := &stubConn{
				setDeadline: func(t time.Time) error {
					if timer != nil {
						timer.Stop()
					}
					if t.IsZero() {
						return nil
					}
					d := time.Until(t)
					if d < 0 {
						deadline <- ioErrDeadline
					} else {
						timer = time.AfterFunc(d, func() {
							deadline <- ioErrDeadline
						})
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

			test.dialer.NetDial = func(ctx context.Context, _, _ string) (net.Conn, error) {
				if t := test.dialDelay; t != 0 {
					delay := time.After(t)
					select {
					case <-delay:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return conn, nil
			}

			ctx := context.Background()
			if t := test.ctxTimeout; t != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, t)
				defer cancel()
			}
			if t := test.ctxCancelAfter; t != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				time.AfterFunc(t, cancel)
			}

			_, _, _, err := test.dialer.Dial(ctx, "ws://gobwas.com")
			if err != test.err {
				t.Fatalf("unexpected error: %q; want %q", err, test.err)
			}
		})
	}
}

func BenchmarkDialer(b *testing.B) {
	for _, test := range []struct {
		dialer Dialer
	}{
		{
			dialer: DefaultDialer,
		},
	} {
		// We need to "mock" the rand.Read method used to generate nonce random
		// bytes for Sec-WebSocket-Key header.
		rand.Seed(0)
		need := b.N * nonceKeySize
		nonceBytes := make([]byte, need)
		n, err := rand.Read(nonceBytes)
		if err != nil {
			b.Fatal(err)
		}
		if n != need {
			b.Fatalf("not enough random nonce bytes: %d; want %d", n, need)
		}
		rand.Seed(0)

		resp := &http.Response{
			StatusCode: http.StatusSwitchingProtocols,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header: http.Header{
				headerConnection: []string{"Upgrade"},
				headerUpgrade:    []string{"websocket"},
				headerSecAccept:  []string{"fill it later"},
			},
		}
		rs := make([][]byte, b.N)
		for i := range rs {
			nonce := make([]byte, nonceSize)
			base64.StdEncoding.Encode(
				nonce,
				nonceBytes[i*nonceKeySize:i*nonceKeySize+nonceKeySize],
			)
			accept := makeAccept(nonce)
			resp.Header[headerSecAccept] = []string{string(accept)}
			rs[i] = dumpResponse(resp)
		}

		var i int
		conn := stubConn{
			read: func(p []byte) (int, error) {
				bts := rs[i]
				if len(p) < len(bts) {
					b.Fatalf("short buffer")
				}
				return copy(p, bts), io.EOF
			},
			write: func(p []byte) (int, error) {
				return len(p), nil
			},
		}
		var nc net.Conn = conn
		test.dialer.NetDial = func(_ context.Context, net, addr string) (net.Conn, error) {
			return nc, nil
		}

		b.ResetTimer()
		for i = 0; i < b.N; i++ {
			_, _, _, err := test.dialer.Dial(context.Background(), "ws://example.org")
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestHostPort(t *testing.T) {
	for _, test := range []struct {
		name string
		host string
		port string

		expHostname string
		expHostport string
	}{
		{
			host:        "foo",
			port:        ":80",
			expHostname: "foo",
			expHostport: "foo:80",
		},
		{
			host:        "foo:1234",
			port:        ":80",
			expHostname: "foo",
			expHostport: "foo:1234",
		},
		{
			name:        "ipv4",
			host:        "127.0.0.1",
			port:        ":80",
			expHostname: "127.0.0.1",
			expHostport: "127.0.0.1:80",
		},
		{
			name:        "ipv4",
			host:        "127.0.0.1:1234",
			port:        ":80",
			expHostname: "127.0.0.1",
			expHostport: "127.0.0.1:1234",
		},
		{
			name:        "ipv6",
			host:        "[0:0:0:0:0:0:0:1]",
			port:        ":80",
			expHostname: "[0:0:0:0:0:0:0:1]",
			expHostport: "[0:0:0:0:0:0:0:1]:80",
		},
		{
			name:        "ipv6",
			host:        "[0:0:0:0:0:0:0:1]:1234",
			port:        ":80",
			expHostname: "[0:0:0:0:0:0:0:1]",
			expHostport: "[0:0:0:0:0:0:0:1]:1234",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			actHostname, actHostport := hostport(test.host, test.port)
			if actHostname != test.expHostname {
				t.Errorf(
					"actual hostname = %q; want %q",
					actHostname, test.expHostname,
				)
			}
			if actHostport != test.expHostport {
				t.Errorf(
					"actual hostname = %q; want %q",
					actHostport, test.expHostport,
				)
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
func (s stubConn) LocalAddr() net.Addr         { return nil }
func (s stubConn) RemoteAddr() net.Addr        { return nil }
func (s stubConn) Close() error {
	if s.close != nil {
		return s.close()
	}
	return nil
}

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
