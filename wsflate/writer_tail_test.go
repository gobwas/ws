package wsflate

import (
	"bytes"
	"compress/flate"
	"io"
	"testing"
)

// blockCompressor is a Compressor that emits an exact, pre-recorded byte
// stream (the blocks, in order) then writes any literal Write() input
// through unchanged. It lets tests pin the exact tail bytes independent of
// the Go toolchain's compress/flate encoder.
type blockCompressor struct {
	dst    io.Writer
	blocks [][]byte
	pos    int
}

func (f *blockCompressor) emit() {
	for f.pos < len(f.blocks) {
		f.dst.Write(f.blocks[f.pos])
		f.pos++
	}
}

func (f *blockCompressor) Write(p []byte) (int, error) {
	f.emit()
	f.dst.Write(p)
	return len(p), nil
}

func (f *blockCompressor) Flush() error { f.emit(); return nil }

func (f *blockCompressor) Close() error { f.emit(); return nil }

// TestCheckTailClassic pins the legacy behaviour: a compressor that ends on
// an empty non-final stored block produces a 0x00 0x00 0xff 0xff held tail,
// which must be dropped (nothing extra written to the destination) and must
// not raise an error.
func TestCheckTailClassic(t *testing.T) {
	// Stream: an empty final fixed block (03 00) followed by an empty
	// non-final stored block (00 00 00 FF FF). The last 4 bytes held by
	// cbuf are therefore 00 00 FF FF (compressionTail).
	var dest bytes.Buffer
	w := NewWriter(&dest, func(w io.Writer) Compressor {
		return &blockCompressor{
			dst:    w,
			blocks: [][]byte{{0x03, 0x00}, {0x00, 0x00, 0x00, 0xff, 0xff}},
		}
	})
	w.Write(nil)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}
	// Flush() then Close() (as CompressTo does) must not double-handle the tail.
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := w.Err(); err != nil {
		t.Fatalf("Writer.Err() = %v; want nil", err)
	}
	// cbuf already flushed the first 3 bytes of the stored block; the final
	// 4 (00 00 ff ff) must be dropped, leaving dest = 03 00 00.
	got := dest.Bytes()
	if want := []byte{0x03, 0x00, 0x00}; !bytes.Equal(got, want) {
		t.Fatalf("dest = % X; want % X", got, want)
	}
}

// TestCheckTailBFINALFixed pins the Go 1.27 behaviour: a compressor that ends
// on an empty final fixed-Huffman block (BFINAL=1, BTYPE=01) produces a
// FF FF 03 00 held tail. checkTail must pass all 4 held bytes through so the
// on-wire payload is a complete, self-terminating DEFLATE stream, and must
// not raise an error. This branch is exercised here on every toolchain, not
// only on Go 1.27.
func TestCheckTailBFINALFixed(t *testing.T) {
	// Stream ends: ... [00 00 00 FF FF] [03 00]
	//   (empty non-final stored block)(empty final fixed block)
	// cbuf holds the last 4 bytes: FF FF 03 00 (tailBFINALFixed).
	var dest bytes.Buffer
	w := NewWriter(&dest, func(w io.Writer) Compressor {
		return &blockCompressor{
			dst:    w,
			blocks: [][]byte{{0x03, 0x00}, {0x00, 0x00, 0x00, 0xff, 0xff}, {0x03, 0x00}},
		}
	})
	w.Write(nil)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := w.Err(); err != nil {
		t.Fatalf("Writer.Err() = %v; want nil", err)
	}
	// Full reconstructed stream: 03 00 00 00 00 FF FF 03 00
	// (9 bytes). The 03 00 final block makes it self-terminating.
	got := dest.Bytes()
	if want := []byte{0x03, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0x03, 0x00}; !bytes.Equal(got, want) {
		t.Fatalf("dest = % X; want % X", got, want)
	}
	// The on-wire payload must round-trip through an RFC 7692 receiver
	// (append 00 00 FF FF, decompress).
	var out bytes.Buffer
	fr := flate.NewReader(io.MultiReader(
		bytes.NewReader(got), bytes.NewReader([]byte{0, 0, 0xff, 0xff}),
	))
	if _, err := io.Copy(&out, fr); err != nil {
		t.Fatalf("decompressing payload: %v", err)
	}
}

// TestCheckTailUnexpected ensures tails matching neither shape are still
// rejected, preserving the original validation.
func TestCheckTailUnexpected(t *testing.T) {
	var dest bytes.Buffer
	w := NewWriter(&dest, func(w io.Writer) Compressor {
		return &blockCompressor{
			dst:    w,
			blocks: [][]byte{{0xaa, 0xbb, 0xcc, 0xdd}},
		}
	})
	w.Write(nil)
	w.Flush()
	if w.Err() == nil {
		t.Fatal("Writer.Err() = nil; want non-nil for unexpected tail")
	}
}

// TestWriterRoundTripDefault is the end-to-end guard that the default
// compress/flate based helper still round-trips on the current toolchain.
func TestWriterRoundTripDefault(t *testing.T) {
	data := []byte("hello, flate!")
	var compressed bytes.Buffer
	if err := DefaultHelper.CompressTo(&compressed, data); err != nil {
		t.Fatalf("CompressTo() error: %v", err)
	}
	var out bytes.Buffer
	if err := DefaultHelper.DecompressTo(&out, compressed.Bytes()); err != nil {
		t.Fatalf("DecompressTo() error: %v", err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("round-trip mismatch: got %q; want %q", out.String(), string(data))
	}
}
