package wsflate

import (
	"bytes"
	"compress/flate"
	"io"
	"strings"
	"testing"
)

func TestEdgeCasesRoundTrip(t *testing.T) {
	cases := map[string][]byte{
		"empty":    {},
		"short":    []byte("hi"),
		"one-byte": []byte("a"),
		"exact-4":  []byte("abcd"),
		"big-text": []byte(strings.Repeat("The quick brown fox. ", 1000)),
	}
	cases["randomish"] = []byte(strings.Repeat("random-lookin data | structure", 300))
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewWriter(&buf, func(w io.Writer) Compressor { f, _ := flate.NewWriter(w, 9); return f })
			w.Write(data)
			w.Flush()
			w.Close()
			if err := w.Err(); err != nil {
				t.Fatalf("Writer.Err(): %v", err)
			}
			var out bytes.Buffer
			r := NewReader(&buf, func(r io.Reader) Decompressor { return flate.NewReader(r) })
			io.Copy(&out, r)
			if err := r.Err(); err != nil {
				t.Fatalf("Reader.Err(): %v (payload=% X)", err, buf.Bytes())
			}
			if !bytes.Equal(out.Bytes(), data) {
				t.Fatalf("mismatch: got %d bytes want %d", out.Len(), len(data))
			}
		})
	}
}

// Each message self-contained (DefaultParameters / no context takeover), with
// fragmented writes and mid-message Flush() calls.
func TestEdgeNoContextTakeover(t *testing.T) {
	msgs := []string{
		"first message with some content",
		"",
		"second message " + strings.Repeat("structure ", 40),
		"tiny",
		"abcd",
	}
	for i, m := range msgs {
		var buf bytes.Buffer
		w := NewWriter(&buf, func(w io.Writer) Compressor { f, _ := flate.NewWriter(w, 9); return f })
		data := []byte(m)
		const chunk = 5
		for len(data) > 0 {
			n := chunk
			if n > len(data) {
				n = len(data)
			}
			w.Write(data[:n])
			data = data[n:]
			if len(data)%2 == 0 {
				if err := w.Flush(); err != nil {
					t.Fatalf("msg %d mid-flush: %v", i, err)
				}
			}
		}
		w.Flush()
		w.Close()
		if err := w.Err(); err != nil {
			t.Fatalf("msg %d writer err: %v (payload=% X)", i, err, buf.Bytes())
		}
		var out bytes.Buffer
		r := NewReader(&buf, func(r io.Reader) Decompressor { return flate.NewReader(r) })
		io.Copy(&out, r)
		if err := r.Err(); err != nil {
			t.Fatalf("msg %d reader err: %v (payload=% X)", i, err, buf.Bytes())
		}
		if out.String() != m {
			t.Fatalf("msg %d: got %q want %q", i, out.String(), m)
		}
	}
}

// Context takeover: ONE long-lived writer. Each message ends on Flush(); the
// final one on Close(). Per RFC 7692 the per-message payloads are byte-aligned
// continuations of a single stream, so we reconstruct each message's payload
// from the wire offsets and feed all payloads (each + 00 00 FF FF) to ONE
// long-lived reader with a carried-over window.
func TestEdgeContextTakeover(t *testing.T) {
	msgs := []string{
		"first message establishes the window with repeatable content ",
		"now this message should reference the previous window content ",
		"and yet another referencing the window ",
		"shorter",
	}
	var wire bytes.Buffer
	w := NewWriter(&wire, func(w io.Writer) Compressor { f, _ := flate.NewWriter(w, 9); return f })
	for i, m := range msgs {
		if _, err := w.Write([]byte(m)); err != nil {
			t.Fatalf("msg %d write: %v", i, err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("msg %d flush: %v", i, err)
		}
		if err := w.Err(); err != nil {
			t.Fatalf("msg %d writer err: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := w.Err(); err != nil {
		t.Fatalf("final writer err: %v (wire=% X)", err, wire.Bytes())
	}

	// Context takeover = one continuous byte-aligned stream, terminated by the
	// final BFINAL block. Feed the whole wire buffer to a single long-lived
	// reader (window carried); no per-message suffix is added.
	var out bytes.Buffer
	r := NewReader(&wire, func(r io.Reader) Decompressor { return flate.NewReader(r) })
	if _, err := io.Copy(&out, r); err != nil {
		t.Fatalf("read: %v (wire=% X)", err, wire.Bytes())
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader err: %v", err)
	}
	want := strings.Join(msgs, "")
	if out.String() != want {
		t.Fatalf("context-takeover concatenation mismatch:\n got %q\nwant %q", out.String(), want)
	}
}
