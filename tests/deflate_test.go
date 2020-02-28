package tests

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsflate"
	"github.com/gobwas/ws/wsutil"
)

func TestFlateClientServer(t *testing.T) {
	e := wsflate.Extension{
		Parameters: wsflate.DefaultParameters,
	}
	client, server := net.Pipe()

	serverDone := make(chan error)
	go func() {
		defer func() {
			client.Close()
			close(serverDone)
		}()
		u := ws.Upgrader{
			Negotiate: e.Negotiate,
		}
		_, err := u.Upgrade(client)
		if err != nil {
			serverDone <- err
			return
		}
		var buf bytes.Buffer
		for {
			frame, err := ws.ReadFrame(client)
			if err != nil {
				serverDone <- err
				return
			}
			frame = ws.UnmaskFrameInPlace(frame)
			frame, err = wsflate.DecompressFrameBuffer(&buf, frame)
			if err != nil {
				serverDone <- err
				return
			}
			echo := ws.NewTextFrame(reverse(frame.Payload))
			if err := ws.WriteFrame(client, echo); err != nil {
				serverDone <- err
				return
			}
			buf.Reset()
		}
	}()

	d := ws.Dialer{
		Extensions: []httphead.Option{
			e.Parameters.Option(),
		},
		NetDial: func(_ context.Context, network, addr string) (net.Conn, error) {
			return server, nil
		},
	}
	dd := wsutil.DebugDialer{
		Dialer: d,
		OnRequest: func(p []byte) {
			t.Logf("Request:\n%s", p)
		},
		OnResponse: func(p []byte) {
			t.Logf("Response:\n%s", p)
		},
	}
	conn, _, _, err := dd.Dial(context.Background(), "ws://stubbed")
	if err != nil {
		t.Fatalf("unexpected Dial() error: %v", err)
	}

	payload := []byte("hello, deflate!")

	frame := ws.NewTextFrame(payload)
	frame, err = wsflate.CompressFrame(frame)
	if err != nil {
		t.Fatalf("can't compress frame: %v", err)
	}
	frame = ws.MaskFrameInPlace(frame)
	if err := ws.WriteFrame(server, frame); err != nil {
		t.Fatalf("unexpected WriteFrame() error: %v", err)
	}

	echo, err := ws.ReadFrame(server)
	if err != nil {
		t.Fatalf("unexpected ReadFrame() error: %v", err)
	}
	if !bytes.Equal(reverse(echo.Payload), payload) {
		t.Fatalf("unexpected echoed bytes")
	}

	conn.Close()

	const timeout = time.Second
	select {
	case <-time.After(timeout):
		t.Fatalf("server goroutine timeout: %s", timeout)

	case err := <-serverDone:
		if err != io.EOF {
			t.Fatalf("unexpected server goroutine error: %v", err)
		}
	}
}

func reverse(buf []byte) []byte {
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return buf
}
