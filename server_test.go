package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	_ "unsafe" // for go:linkname

	"github.com/gobwas/httphead"
)

type upgradeCase struct {
	label string

	protocol  func(string) bool
	extension func(httphead.Option) bool
	onHeader  func(k, v []byte) (error, int)
	onRequest func(h, u []byte) (error, int)

	nonce        []byte
	removeSecKey bool
	badSecKey    bool

	req *http.Request
	res *http.Response
	hs  Handshake
	err error
}

var upgradeCases = []upgradeCase{
	{
		label: "base",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeResponse(101, http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
		}),
	},
	{
		label: "lowercase",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			strings.ToLower(headerUpgrade):    []string{"websocket"},
			strings.ToLower(headerConnection): []string{"Upgrade"},
			strings.ToLower(headerSecVersion): []string{"13"},
		}),
		res: mustMakeResponse(101, http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
		}),
	},
	{
		label:    "uppercase",
		protocol: func(sub string) bool { return true },
		nonce:    mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"WEBSOCKET"},
			headerConnection: []string{"UPGRADE"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeResponse(101, http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
		}),
	},
	{
		label:    "subproto",
		protocol: SelectFromSlice([]string{"b", "d"}),
		nonce:    mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:     []string{"websocket"},
			headerConnection:  []string{"Upgrade"},
			headerSecVersion:  []string{"13"},
			headerSecProtocol: []string{"a", "b", "c", "d"},
		}),
		res: mustMakeResponse(101, http.Header{
			headerUpgrade:     []string{"websocket"},
			headerConnection:  []string{"Upgrade"},
			headerSecProtocol: []string{"b"},
		}),
		hs: Handshake{Protocol: "b"},
	},
	{
		label:    "subproto_comma",
		protocol: SelectFromSlice([]string{"b", "d"}),
		nonce:    mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:     []string{"websocket"},
			headerConnection:  []string{"Upgrade"},
			headerSecVersion:  []string{"13"},
			headerSecProtocol: []string{"a, b, c, d"},
		}),
		res: mustMakeResponse(101, http.Header{
			headerUpgrade:     []string{"websocket"},
			headerConnection:  []string{"Upgrade"},
			headerSecProtocol: []string{"b"},
		}),
		hs: Handshake{Protocol: "b"},
	},
	{
		extension: func(opt httphead.Option) bool {
			switch string(opt.Name) {
			case "b", "d":
				return true
			default:
				return false
			}
		},
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:       []string{"websocket"},
			headerConnection:    []string{"Upgrade"},
			headerSecVersion:    []string{"13"},
			headerSecExtensions: []string{"a;foo=1", "b;bar=2", "c", "d;baz=3"},
		}),
		res: mustMakeResponse(101, http.Header{
			headerUpgrade:       []string{"websocket"},
			headerConnection:    []string{"Upgrade"},
			headerSecExtensions: []string{"b;bar=2,d;baz=3"},
		}),
		hs: Handshake{
			Extensions: []httphead.Option{
				httphead.NewOption("b", map[string]string{
					"bar": "2",
				}),
				httphead.NewOption("d", map[string]string{
					"baz": "3",
				}),
			},
		},
	},

	// Error cases.
	// ------------

	{
		label: "bad_http_method",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("POST", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(405, ErrBadHttpRequestMethod, nil),
		err: ErrBadHttpRequestMethod,
	},
	{
		label: "bad_http_proto",
		nonce: mustMakeNonce(),
		req: setHttpProto(1, 0, mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		})),
		res: mustMakeErrResponse(505, ErrBadHttpProto, nil),
		err: ErrBadHttpProto,
	},
	{
		label: "bad_host",
		nonce: mustMakeNonce(),
		req: withoutHeader("Host", mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		})),
		res: mustMakeErrResponse(400, ErrBadHost, nil),
		err: ErrBadHost,
	},
	{
		label: "bad_upgrade",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadUpgrade, nil),
		err: ErrBadUpgrade,
	},
	{
		label: "bad_upgrade",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			"X-Custom-Header": []string{"value"},
			headerConnection:  []string{"Upgrade"},
			headerSecVersion:  []string{"13"},
		}),
		onRequest: func(_, _ []byte) (error, int) {
			return nil, 0
		},
		onHeader: func(k, v []byte) (error, int) {
			return nil, 0
		},
		res: mustMakeErrResponse(400, ErrBadUpgrade, nil),
		err: ErrBadUpgrade,
	},
	{
		label: "bad_upgrade",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"not-websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadUpgrade, nil),
		err: ErrBadUpgrade,
	},
	{
		label: "bad_connection",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadConnection, nil),
		err: ErrBadConnection,
	},
	{
		label: "bad_connection",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"not-upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadConnection, nil),
		err: ErrBadConnection,
	},
	{
		label: "bad_sec_version_x",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
		}),
		res: mustMakeErrResponse(400, ErrBadSecVersion, nil),
		err: ErrBadSecVersion,
	},
	{
		label: "bad_sec_version",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"upgrade"},
			headerSecVersion: []string{"15"},
		}),
		res: mustMakeErrResponse(426, ErrBadSecVersion, http.Header{
			headerSecVersion: []string{"13"},
		}),
		err: ErrBadSecVersion,
	},
	{
		label:        "bad_sec_key",
		nonce:        mustMakeNonce(),
		removeSecKey: true,
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadSecKey, nil),
		err: ErrBadSecKey,
	},
	{
		label:     "bad_sec_key",
		nonce:     mustMakeNonce(),
		badSecKey: true,
		req: mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadSecKey, nil),
		err: ErrBadSecKey,
	},
}

func TestHTTPUpgrader(t *testing.T) {
	for _, test := range upgradeCases {
		t.Run(test.label, func(t *testing.T) {
			if !test.removeSecKey {
				nonce := test.nonce
				if test.badSecKey {
					nonce = nonce[:nonceSize-1]
				}
				test.req.Header.Set(headerSecKey, string(nonce))
			}
			if test.err == nil {
				test.res.Header.Set(headerSecAccept, string(makeAccept(test.nonce)))
			}

			// Need to emulate http server read request for truth test.
			//
			// We use dumpRequest here because test.req.Write is always send
			// http/1.1 proto version, that does not fits all our testing
			// cases.
			reqBytes := dumpRequest(test.req)
			req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(reqBytes)))
			if err != nil {
				t.Fatal(err)
			}

			res := newRecorder()

			u := HTTPUpgrader{
				Protocol:  test.protocol,
				Extension: test.extension,
			}
			_, _, hs, err := u.Upgrade(req, res, nil)
			if test.err != err {
				t.Errorf(
					"expected error to be '%v', got '%v';\non request:\n====\n%s\n====",
					test.err, err, dumpRequest(req),
				)
				return
			}

			actRespBts := sortHeaders(res.Bytes())
			expRespBts := sortHeaders(dumpResponse(test.res))
			if !bytes.Equal(actRespBts, expRespBts) {
				t.Errorf(
					"unexpected http response:\n---- act:\n%s\n---- want:\n%s\n==== on request:\n%s\n====",
					actRespBts, expRespBts, dumpRequest(test.req),
				)
				return
			}

			if act, exp := hs.Protocol, test.hs.Protocol; act != exp {
				t.Errorf("handshake protocol is %q want %q", act, exp)
			}
			if act, exp := len(hs.Extensions), len(test.hs.Extensions); act != exp {
				t.Errorf("handshake got %d extensions; want %d", act, exp)
			} else {
				for i := 0; i < act; i++ {
					if act, exp := hs.Extensions[i], test.hs.Extensions[i]; !act.Equal(exp) {
						t.Errorf("handshake %d-th extension is %s; want %s", i, act, exp)
					}
				}
			}
		})
	}
}

func TestUpgrader(t *testing.T) {
	for _, test := range upgradeCases {
		t.Run(test.label, func(t *testing.T) {
			if !test.removeSecKey {
				nonce := test.nonce[:]
				if test.badSecKey {
					nonce = nonce[:nonceSize-1]
				}
				test.req.Header.Set(headerSecKey, string(nonce))
			}
			if test.err == nil {
				test.res.Header.Set(headerSecAccept, string(makeAccept(test.nonce)))
			}

			u := Upgrader{
				Protocol: func(p []byte) bool {
					return test.protocol(string(p))
				},
				Extension: func(e httphead.Option) bool {
					return test.extension(e)
				},
				OnHeader:  test.onHeader,
				OnRequest: test.onRequest,
			}

			// We use dumpRequest here because test.req.Write is always send
			// http/1.1 proto version, that does not fits all our testing
			// cases.
			reqBytes := dumpRequest(test.req)
			conn := bytes.NewBuffer(reqBytes)

			hs, err := u.Upgrade(conn)
			if test.err != err {

				t.Errorf("expected error to be '%v', got '%v'", test.err, err)
				return
			}

			actRespBts := sortHeaders(conn.Bytes())
			expRespBts := sortHeaders(dumpResponse(test.res))
			if !bytes.Equal(actRespBts, expRespBts) {
				t.Errorf(
					"unexpected http response:\n---- act:\n%s\n---- want:\n%s\n==== on request:\n%s\n====",
					actRespBts, expRespBts, dumpRequest(test.req),
				)
				return
			}

			if act, exp := hs.Protocol, test.hs.Protocol; act != exp {
				t.Errorf("handshake protocol is %q want %q", act, exp)
			}
			if act, exp := len(hs.Extensions), len(test.hs.Extensions); act != exp {
				t.Errorf("handshake got %d extensions; want %d", act, exp)
			} else {
				for i := 0; i < act; i++ {
					if act, exp := hs.Extensions[i], test.hs.Extensions[i]; !act.Equal(exp) {
						t.Errorf("handshake %d-th extension is %s; want %s", i, act, exp)
					}
				}
			}
		})
	}
}

func BenchmarkHTTPUpgrader(b *testing.B) {
	for _, bench := range upgradeCases {
		bench.req.Header.Set(headerSecKey, string(bench.nonce[:]))

		u := HTTPUpgrader{
			Protocol:  bench.protocol,
			Extension: bench.extension,
		}

		b.Run(bench.label, func(b *testing.B) {
			res := make([]http.ResponseWriter, b.N)
			for i := 0; i < b.N; i++ {
				res[i] = newRecorder()
			}

			i := new(int64)

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					w := res[atomic.AddInt64(i, 1)-1]
					u.Upgrade(bench.req, w, nil)
				}
			})
		})
	}
}

func BenchmarkUpgrader(b *testing.B) {
	for _, bench := range upgradeCases {
		bench.req.Header.Set(headerSecKey, string(bench.nonce[:]))

		u := Upgrader{
			Protocol: func(p []byte) bool {
				return bench.protocol(btsToString(p))
			},
			Extension: func(e httphead.Option) bool {
				return bench.extension(e)
			},
		}

		reqBytes := dumpRequest(bench.req)

		type benchReadWriter struct {
			io.Reader
			io.Writer
		}

		b.Run(bench.label, func(b *testing.B) {
			conn := make([]io.ReadWriter, b.N)
			for i := 0; i < b.N; i++ {
				conn[i] = benchReadWriter{bytes.NewReader(reqBytes), ioutil.Discard}
			}

			i := new(int64)

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					c := conn[atomic.AddInt64(i, 1)-1]
					u.Upgrade(c)
				}
			})
		})
	}
}

func TestHttpStrSelectProtocol(t *testing.T) {
	for i, test := range []struct {
		header string
	}{
		{"jsonrpc, soap, grpc"},
	} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			exp := strings.Split(test.header, ",")
			for i, p := range exp {
				exp[i] = strings.TrimSpace(p)
			}

			var calls []string
			strSelectProtocol(test.header, func(s string) bool {
				calls = append(calls, s)
				return false
			})

			if !reflect.DeepEqual(calls, exp) {
				t.Errorf("selectProtocol(%q, fn); called fn with %v; want %v", test.header, calls, exp)
			}
		})
	}
}

func BenchmarkSelectProtocol(b *testing.B) {
	for _, bench := range []struct {
		label     string
		header    string
		acceptStr func(string) bool
		acceptBts func([]byte) bool
	}{
		{
			label:  "never accept",
			header: "jsonrpc, soap, grpc",
			acceptStr: func(s string) bool {
				return len(s)%2 == 2 // never ok
			},
			acceptBts: func(v []byte) bool {
				return len(v)%2 == 2 // never ok
			},
		},
		{
			label:     "from slice",
			header:    "a, b, c, d, e, f, g",
			acceptStr: SelectFromSlice([]string{"g", "f", "e", "d"}),
		},
		{
			label:     "uniq 1024 from slise",
			header:    strings.Join(randProtocols(1024, 16), ", "),
			acceptStr: SelectFromSlice(randProtocols(1024, 17)),
		},
	} {
		b.Run(fmt.Sprintf("String/%s", bench.label), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				strSelectProtocol(bench.header, bench.acceptStr)
			}
		})
		if bench.acceptBts != nil {
			b.Run(fmt.Sprintf("Bytes/%s", bench.label), func(b *testing.B) {
				h := []byte(bench.header)
				b.StartTimer()

				for i := 0; i < b.N; i++ {
					btsSelectProtocol(h, bench.acceptBts)
				}
			})
		}
	}
}

func randProtocols(n, m int) []string {
	ret := make([]string, n)
	bts := make([]byte, m)
	uniq := map[string]bool{}
	for i := 0; i < n; i++ {
		for {
			for j := 0; j < m; j++ {
				bts[j] = byte(rand.Intn('x'-'a') + 'a')
			}
			str := string(bts)
			if _, has := uniq[str]; !has {
				ret[i] = str
				break
			}
		}
	}
	return ret
}

func dumpRequest(req *http.Request) []byte {
	bts, err := httputil.DumpRequest(req, true)
	if err != nil {
		panic(err)
	}
	return bts
}

func dumpResponse(res *http.Response) []byte {
	cleanClose := !res.Close
	if cleanClose {
		for _, v := range res.Header[headerConnection] {
			if v == "close" {
				cleanClose = false
				break
			}
		}
	}

	bts, err := httputil.DumpResponse(res, true)
	if err != nil {
		panic(err)
	}

	if cleanClose {
		bts = bytes.Replace(bts, []byte("Connection: close\r\n"), nil, -1)
	}

	return bts
}

type headersBytes [][]byte

func (h headersBytes) Len() int           { return len(h) }
func (h headersBytes) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h headersBytes) Less(i, j int) bool { return bytes.Compare(h[i], h[j]) == -1 }

func maskHeader(bts []byte, key, mask string) []byte {
	lines := bytes.Split(bts, []byte("\r\n"))
	for i, line := range lines {
		pair := bytes.Split(line, []byte(": "))
		if string(pair[0]) == key {
			lines[i] = []byte(key + ": " + mask)
		}
	}
	return bytes.Join(lines, []byte("\r\n"))
}

func sortHeaders(bts []byte) []byte {
	lines := bytes.Split(bts, []byte("\r\n"))
	if len(lines) <= 1 {
		return bts
	}
	sort.Sort(headersBytes(lines[1 : len(lines)-2]))
	return bytes.Join(lines, []byte("\r\n"))
}

//go:linkname httpPutBufioReader net/http.putBufioReader
func httpPutBufioReader(*bufio.Reader)

//go:linkname httpPutBufioWriter net/http.putBufioWriter
func httpPutBufioWriter(*bufio.Writer)

//go:linkname httpNewBufioReader net/http.newBufioReader
func httpNewBufioReader(io.Reader) *bufio.Reader

//go:linkname httpNewBufioWriterSize net/http.newBufioWriterSize
func httpNewBufioWriterSize(io.Writer, int) *bufio.Writer

type recorder struct {
	*httptest.ResponseRecorder
	hijacked bool
	conn     func(*bytes.Buffer) net.Conn
}

func newRecorder() *recorder {
	return &recorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (r *recorder) Bytes() []byte {
	if r.hijacked {
		return r.ResponseRecorder.Body.Bytes()
	}
	return dumpResponse(r.Result())
}

func (r *recorder) Hijack() (conn net.Conn, brw *bufio.ReadWriter, err error) {
	if r.hijacked {
		err = fmt.Errorf("already hijacked")
		return
	}

	r.hijacked = true

	var buf *bytes.Buffer
	if r.ResponseRecorder != nil {
		buf = r.ResponseRecorder.Body
	}

	if r.conn != nil {
		conn = r.conn(buf)
	} else {
		conn = stubConn{
			read:  buf.Read,
			write: buf.Write,
			close: func() error { return nil },
		}
	}

	// Use httpNewBufio* linked functions here to make
	// benchmark more closer to real life usage.
	br := httpNewBufioReader(conn)
	bw := httpNewBufioWriterSize(conn, 4<<10)

	brw = bufio.NewReadWriter(br, bw)

	return
}

func mustMakeRequest(method, url string, headers http.Header) *http.Request {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		panic(err)
	}
	req.Header = headers
	return req
}

func setHttpProto(major, minor int, req *http.Request) *http.Request {
	req.ProtoMajor = major
	req.ProtoMinor = minor
	return req
}

func withoutHeader(header string, req *http.Request) *http.Request {
	if strings.EqualFold(header, "Host") {
		req.URL.Host = ""
		req.Host = ""
	} else {
		delete(req.Header, header)
	}
	return req
}

func mustMakeResponse(code int, headers http.Header) *http.Response {
	res := &http.Response{
		StatusCode:    code,
		Status:        http.StatusText(code),
		Header:        headers,
		ProtoMajor:    1,
		ProtoMinor:    1,
		ContentLength: -1,
	}
	return res
}

func mustMakeErrResponse(code int, err error, headers http.Header) *http.Response {
	res := &http.Response{
		StatusCode: code,
		Status:     http.StatusText(code),
		Header: http.Header{
			"Content-Type":           []string{"text/plain; charset=utf-8"},
			"X-Content-Type-Options": []string{"nosniff"},
		},
		ProtoMajor:    1,
		ProtoMinor:    1,
		ContentLength: -1,
	}
	if err != nil {
		res.Body = ioutil.NopCloser(strings.NewReader(err.Error() + "\n"))
	}
	for k, v := range headers {
		res.Header[k] = v
	}
	return res
}

func mustMakeNonce() (ret []byte) {
	ret = make([]byte, nonceSize)
	initNonce(ret)
	return
}

func mustMakeNonceStr() string {
	n := mustMakeNonce()
	return string(n[:])
}
