package wsflate

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"testing"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
)

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, func(w io.Writer) Compressor {
		fw, _ := flate.NewWriter(w, 9)
		return fw
	})
	data := []byte("hello, flate!")
	for _, p := range bytes.SplitAfter(data, []byte{','}) {
		w.Write(p)
		w.Flush()
	}
	if err := w.Close(); err != nil {
		t.Fatalf("unexpected Close() error: %v", err)
	}
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected Writer error: %v", err)
	}

	r := NewReader(&buf, func(r io.Reader) Decompressor {
		return flate.NewReader(r)
	})
	act, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected Reader error: %v", err)
	}
	if exp := data; !bytes.Equal(act, exp) {
		t.Fatalf("unexpected bytes: %#q; want %#q", act, exp)
	}
}

func TestExtensionNegotiation(t *testing.T) {
	client, server := net.Pipe()

	done := make(chan error)
	go func() {
		defer close(done)
		var (
			req bytes.Buffer
			res bytes.Buffer
		)
		conn := struct {
			io.Reader
			io.Writer
		}{
			io.TeeReader(server, &req),
			io.MultiWriter(server, &res),
		}
		e := Extension{
			Parameters: Parameters{
				ServerNoContextTakeover: true,
				ClientNoContextTakeover: true,
			},
		}
		u := ws.Upgrader{
			Negotiate: e.Negotiate,
		}
		hs, err := u.Upgrade(&conn)
		if err != nil {
			done <- err
			return
		}

		p, ok := e.Accepted()
		t.Logf("accepted: %t %+v", ok, p)

		fmt.Println(req.String())
		fmt.Println(res.String())
		t.Logf("server: %+v", hs)
	}()

	d := ws.Dialer{
		Extensions: []httphead.Option{
			(Parameters{
				ServerNoContextTakeover: true,
				ClientNoContextTakeover: true,
				ClientMaxWindowBits:     8,
				ServerMaxWindowBits:     10,
			}).Option(),
			(Parameters{
				ClientMaxWindowBits: 1,
			}).Option(),
			(Parameters{}).Option(),
		},
	}

	uri, err := url.Parse("ws://example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, hs, err := d.Upgrade(client, uri)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if n := len(hs.Extensions); n != 1 {
		t.Fatalf("unexpected number of accepted extensions: %d", n)
	}
	var p Parameters
	if err := p.Parse(hs.Extensions[0]); err != nil {
		t.Fatalf("parse extension error: %v", err)
	}
	t.Logf("client params: %+v", p)
	if err := <-done; err != nil {
		t.Fatalf("server Upgrade() error: %v", err)
	}
}
