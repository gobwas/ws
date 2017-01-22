package ws

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRequestReset(t *testing.T) {
	for i, test := range []struct {
		url        string
		expHost    string
		expPath    string
		protocols  []string
		extensions []string
		headers    http.Header
		err        bool
	}{
		{
			url:        "wss://websocket.com/chat",
			expHost:    "websocket.com",
			expPath:    "/chat",
			protocols:  []string{"subproto", "hello"},
			extensions: []string{"foo; bar=1", "baz"},
			headers: http.Header{
				"Origin": []string{"https://websocket.com"},
			},
		},
		{
			url: "websocket.com/chat",
			err: true,
		},
	} {
		commonHeaders := map[string]string{
			headerConnection: "Upgrade",
			headerUpgrade:    "websocket",
			headerSecVersion: "13",
		}

		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			req := getRequest()
			err := req.Reset(test.url, test.headers, test.protocols, test.extensions)
			if test.err && err == nil {
				t.Errorf("expected error; got nil")
			}
			if !test.err && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.err {
				return
			}

			buf := &bytes.Buffer{}
			if err = req.Write(buf); err != nil {
				t.Errorf("dumping request error: %s", err)
				return
			}

			r, err := http.ReadRequest(bufio.NewReader(buf))
			if err != nil {
				t.Errorf("read request error: %s", err)
				return
			}

			if r.Method != "GET" {
				t.Errorf("http method is %s; want GET", r.Method)
			}
			if r.ProtoMinor != 1 {
				t.Errorf("http proto minor is %d; want 1", r.ProtoMinor)
			}
			if r.ProtoMajor != 1 {
				t.Errorf("http proto major is %d; want 1", r.ProtoMajor)
			}
			if r.URL.Path != test.expPath {
				t.Errorf("http path is %s; want %s", r.URL.Path, test.expPath)
			}

			key := r.Header.Get(headerSecKey)
			bts, err := base64.StdEncoding.DecodeString(key)
			if err != nil {
				t.Errorf("bad %q header: %s", headerSecKey, err)
			}
			if n := len(bts); n != 16 {
				t.Errorf("nonce len is %d; want 16", n)
			}
			r.Header.Del(headerSecKey)

			sub := r.Header.Get(headerSecProtocol)
			protocols := strings.Split(sub, ",")
			for i, p := range protocols {
				protocols[i] = strings.TrimSpace(p)
			}
			if !reflect.DeepEqual(protocols, test.protocols) {
				t.Errorf("%q headers is %v; want %s", headerSecProtocol, protocols, test.protocols)
			}
			r.Header.Del(headerSecProtocol)

			ext := r.Header.Get(headerSecExtensions)
			extensions := strings.Split(ext, ",")
			for i, e := range extensions {
				extensions[i] = strings.TrimSpace(e)
			}
			if !reflect.DeepEqual(extensions, test.extensions) {
				t.Errorf("%q headers is %v; want %s", headerSecExtensions, extensions, test.extensions)
			}
			r.Header.Del(headerSecExtensions)

			for key, exp := range commonHeaders {
				if act := r.Header.Get(key); act != exp {
					t.Errorf("http %q header is %q; want %q", key, act, exp)
				}
				r.Header.Del(key)
			}
			for key, exp := range test.headers {
				if act := r.Header.Get(key); act != exp[0] {
					t.Errorf("http %q custom header is %q; want %q", key, act, exp[0])
				}
				r.Header.Del(key)
			}
			if len(r.Header) != 0 {
				t.Errorf("http request has extra headers:\n\t%v", r.Header)
			}
		})
	}
}

func BenchmarkMakeAccept(b *testing.B) {
	nonce := make([]byte, nonceSize)
	_, err := rand.Read(nonce)
	if err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_ = makeAccept(nonce)
	}
}
