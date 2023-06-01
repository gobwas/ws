package wsutil

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"testing"

	"github.com/gobwas/ws"
)

var bg = context.Background()

func TestDebugDialer(t *testing.T) {
	for _, test := range []struct {
		name string
		resp *http.Response
		body []byte
		err  error
	}{
		{
			name: "base",
		},
		{
			name: "base with footer",
			body: []byte("hello, additional bytes!"),
		},
		{
			name: "fail",
			resp: &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				ProtoMajor: 1,
				ProtoMinor: 1,
			},
			err: ws.ErrHandshakeBadUpgrade,
		},
		{
			name: "fail",
			resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				ProtoMajor: 42,
				ProtoMinor: 1,
			},
			err: ws.ErrHandshakeBadProtocol,
		},
		{
			name: "fail",
			resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				ProtoMajor: 1,
				ProtoMinor: 1,
			},
			err: ws.StatusError(400),
		},
		{
			name: "fail footer",
			resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				ProtoMajor: 1,
				ProtoMinor: 1,
			},
			err: ws.StatusError(400),
		},

		{
			name: "big response",
			// This test expects that even when server sent unsuccessful
			// response with body that does not fit to Dialer read buffer,
			// OnResponse will still be called with full response bytes.
			resp: &http.Response{
				StatusCode: http.StatusOK,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body: ioutil.NopCloser(bytes.NewReader(
					bytes.Repeat([]byte("x"), 5000),
				)),
				ContentLength: 5000,
			},
			// Additional data sent. We expect it will not be shown in
			// OnResponse.
			body: bytes.Repeat([]byte("y"), 1000),
			err:  ws.StatusError(200),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, server := net.Pipe()

			var (
				actReq, actRes []byte
				expReq, expRes []byte
			)
			dd := DebugDialer{
				Dialer: ws.Dialer{
					NetDial: func(_ context.Context, _, _ string) (net.Conn, error) {
						return client, nil
					},
				},
				OnRequest:  func(p []byte) { actReq = p },
				OnResponse: func(p []byte) { actRes = p },
			}
			go func() {
				var (
					reqBuf bytes.Buffer
					resBuf bytes.Buffer
				)
				var (
					tr = io.TeeReader(server, &reqBuf)
					bw = bufio.NewWriterSize(server, 65536)
					mw = io.MultiWriter(bw, &resBuf)
				)
				conn := struct {
					io.Reader
					io.Writer
				}{
					tr, mw,
				}
				if test.resp == nil {
					_, err := ws.Upgrade(conn)
					if err != nil {
						panic(err)
					}
				} else {
					if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
						panic(err)
					}
					if err := test.resp.Write(conn); err != nil {
						panic(err)
					}
				}

				expReq = reqBuf.Bytes()
				expRes = resBuf.Bytes()

				if test.body != nil {
					bw.Write(test.body)
				}
				bw.Flush()
				server.Close()
			}()

			conn, br, _, err := dd.Dial(bg, "ws://stub")
			if err != test.err {
				t.Fatalf("unexpected error: %v; want %v", err, test.err)
			}
			if conn != client {
				t.Errorf("returned connection is non raw")
			}
			if br != nil {
				body, err := ioutil.ReadAll(br)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(body, test.body) {
					t.Errorf("unexpected buffered body: %q; want %q", body, test.body)
				}
			}
			if !bytes.Equal(actReq, expReq) {
				t.Errorf(
					"unexpected request bytes:\nact %d bytes:\n%s\nexp %d bytes:\n%s\n",
					len(actReq), actReq, len(expReq), expReq,
				)
			}
			if !bytes.Equal(actRes, expRes) {
				t.Errorf(
					"unexpected response bytes:\nact %d bytes:\n%s\nexp %d bytes:\n%s\n",
					len(actRes), actRes, len(expRes), expRes,
				)
			}
		})
	}
}
