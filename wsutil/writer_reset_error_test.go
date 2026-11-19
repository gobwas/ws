package wsutil

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gobwas/ws"
)

type resetErrorWriter struct {
	err error
}

func (w resetErrorWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func TestWriterResetAfterError(t *testing.T) {
	sentinel := errors.New("destination failed")
	for _, state := range []struct {
		name  string
		state ws.State
	}{
		{
			name:  "server",
			state: ws.StateServerSide,
		},
		{
			name:  "client",
			state: ws.StateClientSide,
		},
	} {
		for _, failure := range []string{"flush", "fragment", "through", "large write"} {
			t.Run(state.name+"/"+failure, func(t *testing.T) {
				w := NewWriterSize(resetErrorWriter{sentinel}, state.state, ws.OpText, 8)
				var err error
				switch failure {
				case "flush", "fragment":
					_, err = w.Write([]byte("old"))
					if err != nil {
						t.Fatal(err)
					}
					if failure == "flush" {
						err = w.Flush()
					} else {
						err = w.FlushFragment()
					}
				case "through":
					_, err = w.WriteThrough([]byte("old"))
				case "large write":
					_, err = w.Write(bytes.Repeat([]byte("x"), 32))
				}
				if err != sentinel {
					t.Fatalf(
						"initial error = %v, want %v",
						err, sentinel,
					)
				}
				_, err = w.Write([]byte("still old"))
				if err != sentinel {
					t.Fatalf(
						"sticky error = %v, want %v",
						err, sentinel,
					)
				}
				var dst bytes.Buffer
				w.Reset(&dst, state.state, ws.OpBinary)
				payload := []byte("new")
				n, err := w.Write(payload)
				if err != nil || n != len(payload) {
					t.Fatalf(
						"write after Reset = %d, %v",
						n, err,
					)
				}
				err = w.Flush()
				if err != nil {
					t.Fatal(err)
				}
				frame, err := ws.ReadFrame(&dst)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Header.Masked {
					frame = ws.UnmaskFrameInPlace(frame)
				}
				if !frame.Header.Fin || frame.Header.OpCode != ws.OpBinary || !bytes.Equal(frame.Payload, payload) {
					t.Fatalf(
						"frame after Reset = %#v",
						frame,
					)
				}
				if dst.Len() != 0 {
					t.Fatal("extra bytes after frame")
				}
				// A further full reset also discards successful previous message state.
				dst.Reset()
				w.Reset(&dst, state.state, ws.OpText)
				_, err = w.Write([]byte("again"))
				if err != nil {
					t.Fatal(err)
				}
				err = w.Flush()
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestWriterResetOpKeepsError(t *testing.T) {
	sentinel := errors.New("connection failed")
	w := NewWriterSize(resetErrorWriter{sentinel}, ws.StateServerSide, ws.OpText, 8)
	_, err := w.WriteThrough([]byte("old"))
	if err != sentinel {
		t.Fatal(err)
	}
	w.ResetOp(ws.OpBinary)
	_, err = w.Write([]byte("new"))
	if err != sentinel {
		t.Fatalf(
			"ResetOp cleared connection error: %v",
			err,
		)
	}
}

func TestWriterResetForPoolClearsError(t *testing.T) {
	sentinel := errors.New("previous destination failed")
	w := NewWriterSize(resetErrorWriter{sentinel}, ws.StateServerSide, ws.OpText, 128)
	_, err := w.WriteThrough([]byte("old"))
	if err != sentinel {
		t.Fatal(err)
	}
	// This is the reset performed by PutWriter, without relying on sync.Pool
	// returning a particular object or using one after handing it to the pool.
	w.Reset(nil, 0, 0)
	if w.err != nil {
		t.Fatalf(
			"pooled writer retains error: %v",
			w.err,
		)
	}
}

func BenchmarkWriterReset(b *testing.B) {
	w := NewWriterSize(&bytes.Buffer{}, ws.StateServerSide, ws.OpText, 128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset(nil, ws.StateServerSide, ws.OpText)
	}
}
