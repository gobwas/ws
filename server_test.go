package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"reflect"
	"sort"
	"strings"
	"testing"
	_ "unsafe" // for go:linkname
)

func TestUpgrade(t *testing.T) {
	for i, test := range []struct {
		nonce []byte
		req   *http.Request
		res   *http.Response
		hs    Handshake
		cfg   *UpgradeConfig
		err   error
	}{
		{
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
			cfg: &UpgradeConfig{
				Protocol: func(sub string) bool {
					return true
				},
			},
		},
		{
			nonce: mustMakeNonce(),
			req: mustMakeRequest("GET", "ws://example.org", http.Header{
				headerUpgrade:    []string{"WEBSOCKET"},
				headerConnection: []string{"UPGRADE"},
				headerSecVersion: []string{"13"},
			}),
			res: mustMakeResponse(101, http.Header{
				headerUpgrade:    []string{"websocket"},
				headerConnection: []string{"Upgrade"},
			}),
			cfg: &UpgradeConfig{
				Protocol: func(sub string) bool {
					return true
				},
			},
		},
		{
			nonce: mustMakeNonce(),
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
			cfg: &UpgradeConfig{
				Protocol: SelectFromSlice([]string{"b", "d"}),
			},
		},
		// TODO(gobwas) uncomment after selectExtension is ready.
		//{
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
		//	cfg: &UpgradeConfig{
		//		Extension: SelectFromSlice([]string{"b", "d"}),
		//	},
		//},
	} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			if test.nonce != nil {
				test.req.Header.Set(headerSecKey, string(test.nonce))
				test.res.Header.Set(headerSecAccept, string(makeAccept(test.nonce)))
			}

			res := newRecorder()
			_, _, hs, err := Upgrade(test.req, res, test.cfg)
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

func BenchmarkUpgrade(b *testing.B) {
	bts101 := []byte("HTTP/1.1 101")
	for _, bench := range []struct {
		label string
		req   *http.Request
		cfg   *UpgradeConfig
	}{
		{
			label: "base",
			req: mustMakeRequest("GET", "ws://example.org", http.Header{
				headerUpgrade:    []string{"websocket"},
				headerConnection: []string{"Upgrade"},
				headerSecVersion: []string{"13"},
				headerSecKey:     []string{string(mustMakeNonce())},
			}),
			cfg: &UpgradeConfig{
				Protocol: func(sub string) bool {
					return true
				},
			},
		},
		{
			label: "uppercase",
			req: mustMakeRequest("GET", "ws://example.org", http.Header{
				headerUpgrade:    []string{"WEBSOCKET"},
				headerConnection: []string{"UPGRADE"},
				headerSecVersion: []string{"13"},
				headerSecKey:     []string{string(mustMakeNonce())},
			}),
			cfg: &UpgradeConfig{
				Protocol: func(sub string) bool {
					return true
				},
			},
		},
	} {
		b.Run(bench.label, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					res := newRecorder()
					_, _, _, err := Upgrade(bench.req, res, bench.cfg)
					if err != nil {
						b.Fatal(err)
					}
					if !bytes.HasPrefix(res.Body.Bytes(), bts101) {
						b.Fatalf("unexpected http status code: %v\n%s", res.Code, res.Body.String())
					}
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
			if has := hasToken(test.header, test.token); has != test.exp {
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
				_ = hasToken(bench.header, bench.token)
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

func mustMakeNonce() []byte {
	b := make([]byte, nonceSize)
	newNonce(b)
	return b
}
