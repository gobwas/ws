package ws

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var readLineCases = []struct {
	label   string
	in      string
	line    []byte
	err     error
	bufSize int
}{
	{
		label:   "simple",
		in:      "hello, world!",
		line:    []byte("hello, world!"),
		err:     io.EOF,
		bufSize: 1024,
	},
	{
		label:   "simple",
		in:      "hello, world!\r\n",
		line:    []byte("hello, world!"),
		bufSize: 1024,
	},
	{
		label:   "simple",
		in:      "hello, world!\n",
		line:    []byte("hello, world!"),
		bufSize: 1024,
	},
	{
		// The case where "\r\n" straddles the buffer.
		label:   "straddle",
		in:      "hello, world!!!\r\n...",
		line:    []byte("hello, world!!!"),
		bufSize: 16,
	},
	{
		label:   "chunked",
		in:      "hello, world! this is a long long line!",
		line:    []byte("hello, world! this is a long long line!"),
		err:     io.EOF,
		bufSize: 16,
	},
	{
		label:   "chunked",
		in:      "hello, world! this is a long long line!\r\n",
		line:    []byte("hello, world! this is a long long line!"),
		bufSize: 16,
	},
}

func TestReadLine(t *testing.T) {
	for _, test := range readLineCases {
		t.Run(test.label, func(t *testing.T) {
			br := bufio.NewReaderSize(strings.NewReader(test.in), test.bufSize)
			bts, err := readLine(br)
			if err != test.err {
				t.Errorf("unexpected error: %v; want %v", err, test.err)
			}
			if act, exp := bts, test.line; !bytes.Equal(act, exp) {
				t.Errorf("readLine() result is %#q; want %#q", act, exp)
			}
		})
	}
}

func BenchmarkReadLine(b *testing.B) {
	for _, test := range readLineCases {
		sr := strings.NewReader(test.in)
		br := bufio.NewReaderSize(sr, test.bufSize)
		b.Run(test.label, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = readLine(br)
				sr.Reset(test.in)
				br.Reset(sr)
			}
		})
	}
}

func TestUpgradeSlowClient(t *testing.T) {
	for _, test := range []struct {
		lim *limitWriter
	}{
		{
			lim: &limitWriter{
				Bandwidth: 100,
				Period:    time.Second,
				Burst:     10,
			},
		},
		{
			lim: &limitWriter{
				Bandwidth: 100,
				Period:    time.Second,
				Burst:     100,
			},
		},
	} {
		t.Run("", func(t *testing.T) {
			client, server, err := socketPair()
			if err != nil {
				t.Fatal(err)
			}
			test.lim.Dest = server

			header := http.Header{
				"X-Websocket-Test-1": []string{"Yes"},
				"X-Websocket-Test-2": []string{"Yes"},
				"X-Websocket-Test-3": []string{"Yes"},
				"X-Websocket-Test-4": []string{"Yes"},
			}
			d := Dialer{
				NetDial: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return connWithWriter{server, test.lim}, nil
				},
				Header: HandshakeHeaderHTTP(header),
			}
			var (
				expHost = "example.org"
				expURI  = "/path/to/ws"
			)
			receivedHeader := http.Header{}
			u := Upgrader{
				OnRequest: func(uri []byte) error {
					if u := string(uri); u != expURI {
						t.Errorf(
							"unexpected URI in OnRequest() callback: %q; want %q",
							u, expURI,
						)
					}
					return nil
				},
				OnHost: func(host []byte) error {
					if h := string(host); h != expHost {
						t.Errorf(
							"unexpected host in OnRequest() callback: %q; want %q",
							h, expHost,
						)
					}
					return nil
				},
				OnHeader: func(key, value []byte) error {
					receivedHeader.Add(string(key), string(value))
					return nil
				},
			}
			upgrade := make(chan error, 1)
			go func() {
				_, err := u.Upgrade(client)
				upgrade <- err
			}()

			_, _, _, err = d.Dial(context.Background(), "ws://"+expHost+expURI)
			if err != nil {
				t.Errorf("Dial() error: %v", err)
			}

			if err := <-upgrade; err != nil {
				t.Errorf("Upgrade() error: %v", err)
			}
			for key, values := range header {
				act, has := receivedHeader[key]
				if !has {
					t.Errorf("OnHeader() was not called with %q header key", key)
				}
				if !reflect.DeepEqual(act, values) {
					t.Errorf("OnHeader(%q) different values: %v; want %v", key, act, values)
				}
			}
		})
	}
}

type connWithWriter struct {
	net.Conn
	w io.Writer
}

func (w connWithWriter) Write(p []byte) (int, error) {
	return w.w.Write(p)
}

type limitWriter struct {
	Dest      io.Writer
	Bandwidth int
	Burst     int
	Period    time.Duration

	mu      sync.Mutex
	cond    sync.Cond
	once    sync.Once
	done    chan struct{}
	tickets int
}

func (w *limitWriter) init() {
	w.once.Do(func() {
		w.cond.L = &w.mu
		w.done = make(chan struct{})

		tick := w.Period / time.Duration(w.Bandwidth)
		go func() {
			t := time.NewTicker(tick)
			for {
				select {
				case <-t.C:
					w.mu.Lock()
					w.tickets = w.Burst
					w.mu.Unlock()
					w.cond.Signal()
				case <-w.done:
					t.Stop()
					return
				}
			}
		}()
	})
}

func (w *limitWriter) allow(n int) (allowed int) {
	w.init()
	w.mu.Lock()
	defer w.mu.Unlock()
	for w.tickets == 0 {
		w.cond.Wait()
	}
	if w.tickets < 0 {
		return -1
	}
	allowed = min(w.tickets, n)
	w.tickets -= allowed
	return allowed
}

func (w *limitWriter) Close() error {
	w.init()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tickets < 0 {
		return nil
	}
	w.tickets = -1
	close(w.done)
	w.cond.Broadcast()
	return nil
}

func (w *limitWriter) Write(p []byte) (n int, err error) {
	w.init()
	for n < len(p) {
		m := w.allow(len(p))
		if m < 0 {
			return 0, io.ErrClosedPipe
		}
		if _, err := w.Dest.Write(p[n : n+m]); err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

func socketPair() (client, server net.Conn, err error) {
	ln, err := net.Listen("tcp", "localhost:")
	if err != nil {
		return nil, nil, err
	}
	type connAndError struct {
		conn net.Conn
		err  error
	}
	dial := make(chan connAndError, 1)
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		dial <- connAndError{conn, err}
	}()
	server, err = ln.Accept()
	if err != nil {
		return nil, nil, err
	}
	ce := <-dial
	if err := ce.err; err != nil {
		return nil, nil, err
	}
	return ce.conn, server, nil
}

func TestHasToken(t *testing.T) {
	for i, test := range []struct {
		header string
		token  string
		exp    bool
	}{
		{"Keep-Alive, Close, Upgrade", "upgrade", true},
		{"Keep-Alive, Close, upgrade, hello", "upgrade", true},
		{"Keep-Alive, Close,  hello", "upgrade", false},
	} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			if has := strHasToken(test.header, test.token); has != test.exp {
				t.Errorf("hasToken(%q, %q) = %v; want %v", test.header, test.token, has, test.exp)
			}
		})
	}
}

func BenchmarkHasToken(b *testing.B) {
	for i, bench := range []struct {
		header string
		token  string
	}{
		{"Keep-Alive, Close, Upgrade", "upgrade"},
		{"Keep-Alive, Close, upgrade, hello", "upgrade"},
		{"Keep-Alive, Close,  hello", "upgrade"},
	} {
		b.Run(fmt.Sprintf("#%d", i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = strHasToken(bench.header, bench.token)
			}
		})
	}
}

func TestAsciiToInt(t *testing.T) {
	for _, test := range []struct {
		bts []byte
		exp int
		err bool
	}{
		{[]byte{'0'}, 0, false},
		{[]byte{'1'}, 1, false},
		{[]byte("42"), 42, false},
		{[]byte("420"), 420, false},
		{[]byte("010050042"), 10050042, false},
	} {
		t.Run(string(test.bts), func(t *testing.T) {
			act, err := asciiToInt(test.bts)
			if (test.err && err == nil) || (!test.err && err != nil) {
				t.Errorf("unexpected error: %v", err)
			}
			if act != test.exp {
				t.Errorf("asciiToInt(%v) = %v; want %v", test.bts, act, test.exp)
			}
		})
	}
}

func TestBtrim(t *testing.T) {
	for _, test := range []struct {
		bts []byte
		exp []byte
	}{
		{[]byte("abc"), []byte("abc")},
		{[]byte(" abc"), []byte("abc")},
		{[]byte("abc "), []byte("abc")},
		{[]byte(" abc "), []byte("abc")},
	} {
		t.Run(string(test.bts), func(t *testing.T) {
			if act := btrim(test.bts); !bytes.Equal(act, test.exp) {
				t.Errorf("btrim(%v) = %v; want %v", test.bts, act, test.exp)
			}
		})
	}
}

func TestBSplit3(t *testing.T) {
	for _, test := range []struct {
		bts  []byte
		sep  byte
		exp1 []byte
		exp2 []byte
		exp3 []byte
	}{
		{[]byte(""), ' ', []byte{}, nil, nil},
		{[]byte("GET / HTTP/1.1"), ' ', []byte("GET"), []byte("/"), []byte("HTTP/1.1")},
	} {
		t.Run(string(test.bts), func(t *testing.T) {
			b1, b2, b3 := bsplit3(test.bts, test.sep)
			if !bytes.Equal(b1, test.exp1) || !bytes.Equal(b2, test.exp2) || !bytes.Equal(b3, test.exp3) {
				t.Errorf(
					"bsplit3(%q) = %q, %q, %q; want %q, %q, %q",
					string(test.bts), string(b1), string(b2), string(b3),
					string(test.exp1), string(test.exp2), string(test.exp3),
				)
			}
		})
	}
}

var canonicalHeaderCases = [][]byte{
	[]byte("foo-"),
	[]byte("-foo"),
	[]byte("-"),
	[]byte("foo----bar"),
	[]byte("foo-bar"),
	[]byte("FoO-BaR"),
	[]byte("Foo-Bar"),
	[]byte("sec-websocket-extensions"),
}

func TestCanonicalizeHeaderKey(t *testing.T) {
	for _, bts := range canonicalHeaderCases {
		t.Run(string(bts), func(t *testing.T) {
			act := append([]byte(nil), bts...)
			canonicalizeHeaderKey(act)

			exp := strToBytes(textproto.CanonicalMIMEHeaderKey(string(bts)))

			if !bytes.Equal(act, exp) {
				t.Errorf(
					"canonicalizeHeaderKey(%v) = %v; want %v",
					string(bts), string(act), string(exp),
				)
			}
		})
	}
}

func BenchmarkCanonicalizeHeaderKey(b *testing.B) {
	for _, bts := range canonicalHeaderCases {
		b.Run(string(bts), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				canonicalizeHeaderKey(bts)
			}
		})
	}
}
