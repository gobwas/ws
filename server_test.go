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
)

type upgradeCase struct {
	label string

	protocol, extension func(string) bool

	nonce [nonceSize]byte
	req   *http.Request
	res   *http.Response
	hs    Handshake
	err   error
}

var upgradeCases = []upgradeCase{
	{
		label:    "lowercase",
		protocol: func(sub string) bool { return true },
		nonce:    mustMakeNonce(),
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
	// TODO(gobwas) uncomment after selectExtension is ready.
	//{
	//	extension: SelectFromSlice([]string{"b", "d"}),
	//	nonce: mustMakeNonce(),
	//	req: mustMakeRequest("GET", "ws://example.org", http.Header{
	//		headerUpgrade:       []string{"websocket"},
	//		headerConnection:    []string{"Upgrade"},
	//		headerSecVersion:    []string{"13"},
	//		headerSecExtensions: []string{"a", "b", "c", "d"},
	//	}),
	//	res: mustMakeResponse(101, http.Header{
	//		headerUpgrade:       []string{"websocket"},
	//		headerConnection:    []string{"Upgrade"},
	//		headerSecExtensions: []string{"b", "d"},
	//	}),
	//  hs: Handshake{Extensions: ["b", "d"]},
	//},

	// Error cases.
	// ------------

	{
		label: "err_bad_http_method",
		nonce: mustMakeNonce(),
		req: mustMakeRequest("POST", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		}),
		res: mustMakeErrResponse(400, ErrBadHttpRequestMethod, nil),
		err: ErrBadHttpRequestMethod,
	},
	{
		label: "err_bad_http_proto",
		nonce: mustMakeNonce(),
		req: setHttpProto(1, 0, mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"13"},
		})),
		res: mustMakeErrResponse(400, ErrBadHttpRequestProto, nil),
		err: ErrBadHttpRequestProto,
	},
	{
		label: "err_bad_sec_version",
		nonce: mustMakeNonce(),
		req: setHttpProto(1, 1, mustMakeRequest("GET", "ws://example.org", http.Header{
			headerUpgrade:    []string{"websocket"},
			headerConnection: []string{"Upgrade"},
			headerSecVersion: []string{"15"},
		})),
		res: mustMakeErrResponse(426, ErrBadSecVersion, http.Header{
			headerSecVersion: []string{"13"},
		}),
		err: ErrBadSecVersion,
	},
}

func TestUpgrader(t *testing.T) {
	for _, test := range upgradeCases {
		t.Run(test.label, func(t *testing.T) {
			test.req.Header.Set(headerSecKey, string(test.nonce[:]))
			if test.err == nil {
				test.res.Header.Set(headerSecAccept, makeAccept(test.nonce))
			}

			u := Upgrader{
				Protocol:  test.protocol,
				Extension: test.extension,
			}

			res := newRecorder()
			_, _, hs, err := u.Upgrade(test.req, res, nil)
			if test.err != err {
				t.Errorf("expected error to be '%v', got '%v'", test.err, err)
				return
			}

			actRespBts := sortHeaders(res.Bytes())
			expRespBts := sortHeaders(dumpResponse(test.res))
			if !bytes.Equal(actRespBts, expRespBts) {
				t.Errorf(
					"unexpected http response:\n---- act:\n%s\n---- want:\n%s\n====",
					actRespBts, expRespBts,
				)
				return
			}

			if !reflect.DeepEqual(hs, test.hs) {
				t.Errorf("unexpected handshake: %#v; want %#v", hs, test.hs)
			}
		})
	}
}

func TestConnUpgrader(t *testing.T) {
	for _, test := range upgradeCases {
		t.Run(test.label, func(t *testing.T) {
			test.req.Header.Set(headerSecKey, string(test.nonce[:]))
			if test.err == nil {
				test.res.Header.Set(headerSecAccept, makeAccept(test.nonce))
			}

			u := ConnUpgrader{
				Protocol: func(p []byte) bool {
					return test.protocol(string(p))
				},
				Extension: func(e []byte) bool {
					return test.extension(string(e))
				},
			}

			// We use dumpRequest here because test.req.Write is always send
			// http/1.1 proto version, that does not fits all our testing
			// cases.
			reqBytes := dumpRequest(test.req)
			conn := bytes.NewBuffer(reqBytes)

			hs, err := u.Upgrade(conn, nil)
			if test.err != err {
				t.Errorf("expected error to be '%v', got '%v'", test.err, err)
				return
			}

			actRespBts := sortHeaders(conn.Bytes())
			expRespBts := sortHeaders(dumpResponse(test.res))
			if !bytes.Equal(actRespBts, expRespBts) {
				t.Errorf(
					"unexpected http response:\n---- act:\n%s\n---- want:\n%s\n====",
					actRespBts, expRespBts,
				)
				return
			}

			if !reflect.DeepEqual(hs, test.hs) {
				t.Errorf("unexpected handshake: %#v; want %#v", hs, test.hs)
			}
		})
	}
}

func BenchmarkUpgrader(b *testing.B) {
	for _, bench := range upgradeCases {
		bench.req.Header.Set(headerSecKey, string(bench.nonce[:]))

		u := Upgrader{
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
					_, _, _, err := u.Upgrade(bench.req, w, nil)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkConnUpgrader(b *testing.B) {
	for _, bench := range upgradeCases {
		bench.req.Header.Set(headerSecKey, string(bench.nonce[:]))

		u := ConnUpgrader{
			Protocol: func(p []byte) bool {
				return bench.protocol(btsToString(p))
			},
			Extension: func(e []byte) bool {
				return bench.extension(btsToString(e))
			},
		}

		buf := &bytes.Buffer{}
		bench.req.Write(buf)

		bts := buf.Bytes()

		type benchReadWriter struct {
			io.Reader
			io.Writer
		}

		b.Run(bench.label, func(b *testing.B) {
			conn := make([]io.ReadWriter, b.N)
			for i := 0; i < b.N; i++ {
				conn[i] = benchReadWriter{bytes.NewReader(bts), ioutil.Discard}
			}

			i := new(int64)

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					c := conn[atomic.AddInt64(i, 1)-1]
					u.Upgrade(c, nil)
				}
			})
		})
	}
}

func TestSelectProtocol(t *testing.T) {
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
			selectProtocol(test.header, func(s string) bool {
				calls = append(calls, s)
				return false
			})

			if !reflect.DeepEqual(calls, exp) {
				t.Errorf("selectProtocol(%q, fn); called fn with %v; want %v", test.header, calls, exp)
			}
		})
	}
}

func TestSelectExtensions(t *testing.T) {

}

func BenchmarkSelectProtocol(b *testing.B) {
	for _, bench := range []struct {
		label  string
		header string
		accept func(string) bool
	}{
		{
			label:  "never accept",
			header: "jsonrpc, soap, grpc",
			accept: func(s string) bool {
				return len(s)%2 == 2 // never ok
			},
		},
		{
			label:  "from slice",
			header: "a, b, c, d, e, f, g",
			accept: SelectFromSlice([]string{"g", "f", "e", "d"}),
		},
		{
			label:  "uniq 1024 from slise",
			header: strings.Join(randProtocols(1024, 16), ", "),
			accept: SelectFromSlice(randProtocols(1024, 17)),
		},
	} {
		b.Run(fmt.Sprintf("#%s_optimized", bench.label), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				selectProtocol(bench.header, bench.accept)
			}
		})
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
func (h headersBytes) Less(i, j int) bool { return string(h[i]) < string(h[j]) }

func sortHeaders(bts []byte) []byte {
	lines := bytes.Split(bts, []byte("\r\n"))
	if len(lines) <= 1 {
		return bts
	}
	sort.Sort(headersBytes(lines[1 : len(lines)-2]))
	return bytes.Join(lines, []byte("\r\n"))
}

type recorder struct {
	*httptest.ResponseRecorder
	hijacked bool
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

//go:linkname httpPutBufioReader net/http.putBufioReader
func httpPutBufioReader(*bufio.Reader)

//go:linkname httpPutBufioWriter net/http.putBufioWriter
func httpPutBufioWriter(*bufio.Writer)

//go:linkname httpNewBufioReader net/http.newBufioReader
func httpNewBufioReader(io.Reader) *bufio.Reader

//go:linkname httpNewBufioWriterSize net/http.newBufioWriterSize
func httpNewBufioWriterSize(io.Writer, int) *bufio.Writer

func (r *recorder) Hijack() (conn net.Conn, brw *bufio.ReadWriter, err error) {
	if r.hijacked {
		err = fmt.Errorf("already hijacked")
		return
	}

	r.hijacked = true

	buf := r.ResponseRecorder.Body

	conn = stubConn{
		read:  buf.Read,
		write: buf.Write,
		close: func() error { return nil },
	}

	// Use httpNewBufio* linked functions here to make
	// benchmark more closer to real life usage.
	br := httpNewBufioReader(buf)
	bw := httpNewBufioWriterSize(buf, 4<<10)

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

func mustMakeNonce() (ret [nonceSize]byte) {
	newNonce(ret[:])
	return
}

func mustMakeNonceStr() string {
	n := mustMakeNonce()
	return string(n[:])
}
