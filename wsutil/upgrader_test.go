package wsutil

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"testing"

	"github.com/gobwas/ws"
)

func TestDebugUpgrader(t *testing.T) {
	for _, test := range []struct {
		name     string
		upgrader ws.Upgrader
		req      []byte
	}{
		{
			// Base case.
		},
		{
			req: []byte("" +
				"GET /test HTTP/1.1\r\n" +
				"Host: example.org\r\n" +
				"\r\n",
			),
		},
		{
			req: []byte("PUT /fail HTTP/1.1\r\n\r\n"),
		},
		{
			req: []byte("GET /fail HTTP/1.0\r\n\r\n"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var (
				reqBuf bytes.Buffer
				resBuf bytes.Buffer

				expReq, expRes []byte
				actReq, actRes []byte
			)
			if test.req == nil {
				var dialer ws.Dialer
				dialer.Upgrade(struct {
					io.Reader
					io.Writer
				}{
					new(falseReader),
					&reqBuf,
				}, makeURL("wss://example.org"))
			} else {
				reqBuf.Write(test.req)
			}

			// Need to save bytes before they will be read by Upgrade().
			expReq = reqBuf.Bytes()

			du := DebugUpgrader{
				Upgrader:   test.upgrader,
				OnRequest:  func(p []byte) { actReq = p },
				OnResponse: func(p []byte) { actRes = p },
			}
			du.Upgrade(struct {
				io.Reader
				io.Writer
			}{
				&reqBuf,
				&resBuf,
			})

			expRes = resBuf.Bytes()

			if !bytes.Equal(actReq, expReq) {
				t.Errorf(
					"unexpected request bytes:\nact:\n%s\nwant:\n%s\n",
					actReq, expReq,
				)
			}
			if !bytes.Equal(actRes, expRes) {
				t.Errorf(
					"unexpected response bytes:\nact:\n%s\nwant:\n%s\n",
					actRes, expRes,
				)
			}
		})
	}
}

type falseReader struct{}

func (f falseReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("falsy read")
}

func makeURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
