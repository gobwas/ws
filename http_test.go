package ws

import (
	"bufio"
	"io/ioutil"
	"net/textproto"
	"net/url"
	"testing"

	"github.com/gobwas/httphead"
)

type httpVersionCase struct {
	in    []byte
	major int
	minor int
	ok    bool
}

var httpVersionCases = []httpVersionCase{
	{[]byte("HTTP/1.1"), 1, 1, true},
	{[]byte("HTTP/1.0"), 1, 0, true},
	{[]byte("HTTP/1.2"), 1, 2, true},
	{[]byte("HTTP/42.1092"), 42, 1092, true},
}

func TestParseHttpVersion(t *testing.T) {
	for _, c := range httpVersionCases {
		t.Run(string(c.in), func(t *testing.T) {
			major, minor, ok := httpParseVersion(c.in)
			if major != c.major || minor != c.minor || ok != c.ok {
				t.Errorf(
					"parseHttpVersion([]byte(%q)) = %v, %v, %v; want %v, %v, %v",
					string(c.in), major, minor, ok, c.major, c.minor, c.ok,
				)
			}
		})
	}
}

func TestHeaderNames(t *testing.T) {
	testCases := []struct {
		have, want string
	}{
		{
			have: headerHost,
			want: headerHostCanonical,
		},
		{
			have: headerUpgrade,
			want: headerUpgradeCanonical,
		},
		{
			have: headerConnection,
			want: headerConnectionCanonical,
		},
		{
			have: headerSecVersion,
			want: headerSecVersionCanonical,
		},
		{
			have: headerSecProtocol,
			want: headerSecProtocolCanonical,
		},
		{
			have: headerSecExtensions,
			want: headerSecExtensionsCanonical,
		},
		{
			have: headerSecKey,
			want: headerSecKeyCanonical,
		},
		{
			have: headerSecAccept,
			want: headerSecAcceptCanonical,
		},
	}

	for _, tc := range testCases {
		if have := textproto.CanonicalMIMEHeaderKey(tc.have); have != tc.want {
			t.Errorf("have %q want %q,", have, tc.want)
		}
	}
}

func BenchmarkParseHttpVersion(b *testing.B) {
	for _, c := range httpVersionCases {
		b.Run(string(c.in), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _, _ = httpParseVersion(c.in)
			}
		})
	}
}

func BenchmarkHttpWriteUpgradeRequest(b *testing.B) {
	for _, test := range []struct {
		url        *url.URL
		protocols  []string
		extensions []httphead.Option
		headers    HandshakeHeaderFunc
	}{
		{
			url: makeURL("ws://example.org"),
		},
	} {
		bw := bufio.NewWriter(ioutil.Discard)
		nonce := make([]byte, nonceSize)
		initNonce(nonce)

		var headers HandshakeHeader
		if test.headers != nil {
			headers = test.headers
		}

		b.ResetTimer()
		b.Run("", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				httpWriteUpgradeRequest(bw,
					test.url,
					nonce,
					test.protocols,
					test.extensions,
					headers,
				)
			}
		})
	}
}

func makeURL(s string) *url.URL {
	ret, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return ret
}
