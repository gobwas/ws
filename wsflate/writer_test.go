package wsflate

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"net"
	"net/url"
	"testing"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
)

// fakeCompressor is a Compressor which passes data through as is, writes
// given bytes on Flush() and Close(), and remembers whether it was closed.
type fakeCompressor struct {
	dst    io.Writer
	flush  []byte
	close  []byte
	closed bool
}

func (c *fakeCompressor) Write(p []byte) (int, error) {
	return c.dst.Write(p)
}

func (c *fakeCompressor) Flush() error {
	_, err := c.dst.Write(c.flush)
	return err
}

func (c *fakeCompressor) Close() error {
	c.closed = true
	_, err := c.dst.Write(c.close)
	return err
}

func TestWriterClose(t *testing.T) {
	// Go 1.27's flate writer terminates the stream with a short fixed-Huffman
	// block instead of the empty stored block emitted by older versions. Both
	// must stay out of the message.
	for _, test := range []struct {
		name  string
		close []byte
		flush bool
		bad   bool
	}{
		{
			name:  "stored final block, flushed",
			close: []byte{0x01, 0x00, 0x00, 0xff, 0xff},
			flush: true,
		},
		{
			name:  "stored final block, not flushed",
			close: []byte{0x01, 0x00, 0x00, 0xff, 0xff},
		},
		{
			name:  "fixed final block, flushed",
			close: []byte{0x03, 0x00},
			flush: true,
		},
		{
			name:  "fixed final block, not flushed",
			close: []byte{0x03, 0x00},
		},
		{
			name:  "no final block, not flushed",
			close: nil,
		},
		{
			name: "bad compressor",
			bad:  true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			var compressor *fakeCompressor
			w := NewWriter(&buf, func(dst io.Writer) Compressor {
				compressor = &fakeCompressor{
					dst:   dst,
					flush: compressionTail[:],
					close: test.close,
				}
				if test.bad {
					// Does not sync flush.
					compressor.flush = nil
				}
				return compressor
			})
			if _, err := w.Write([]byte("payload")); err != nil {
				t.Fatalf("unexpected Write() error: %v", err)
			}
			if test.flush {
				if err := w.Flush(); err != nil {
					t.Fatalf("unexpected Flush() error: %v", err)
				}
			}
			err := w.Close()
			if test.bad {
				if err == nil {
					t.Fatalf("expected Close() error for bad compressor")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected Close() error: %v", err)
			}
			if !compressor.closed {
				t.Fatalf("Close() did not close the compressor")
			}
			if got, want := buf.String(), "payload"; got != want {
				t.Fatalf("unexpected output: %q; want %q", got, want)
			}
		})
	}
}

// TestWriterCloseFlate checks that with the real compress/flate the message
// bytes do not depend on whether Flush(), Close() or both were called.
func TestWriterCloseFlate(t *testing.T) {
	type config struct {
		flush bool
		close bool
	}
	var (
		data    = []byte("hello, flate!")
		configs = []config{
			{
				flush: true,
			},
			{
				close: true,
			},
			{
				flush: true,
				close: true,
			},
		}
		// Message bytes produced by each config and, inversely, the number
		// of configs produced each message; all of them must agree.
		output = make(map[config]string, len(configs))
		seen   = make(map[string]int, len(configs))
	)
	for _, c := range configs {
		var buf bytes.Buffer
		w := NewWriter(&buf, func(w io.Writer) Compressor {
			fw, _ := flate.NewWriter(w, 9)
			return fw
		})
		if _, err := w.Write(data); err != nil {
			t.Fatalf("%+v: unexpected Write() error: %v", c, err)
		}
		if c.flush {
			if err := w.Flush(); err != nil {
				t.Fatalf("%+v: unexpected Flush() error: %v", c, err)
			}
		}
		if c.close {
			if err := w.Close(); err != nil {
				t.Fatalf("%+v: unexpected Close() error: %v", c, err)
			}
		}
		output[c] = buf.String()
		seen[output[c]]++

		// Must be decompressible as a PMCE message.
		r := NewReader(&buf, func(r io.Reader) Decompressor {
			return flate.NewReader(r)
		})
		p, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("%+v: unexpected Reader error: %v", c, err)
		}
		if !bytes.Equal(p, data) {
			t.Fatalf("%+v: unexpected bytes: %#q; want %#q", c, p, data)
		}
	}
	if len(seen) > 1 {
		for _, c := range configs {
			t.Errorf("%+v: %x (seen %d times)", c, output[c], seen[output[c]])
		}
		t.Fatalf("Flush()/Close() combinations produced %d different outputs", len(seen))
	}
}

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
	act, err := io.ReadAll(r)
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
