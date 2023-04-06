package wsutil

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"testing"
	"unsafe"

	"github.com/gobwas/ws"
)

// TODO(gobwas): test NewWriterSize on edge cases for offset.

const (
	bitsize = 32 << (^uint(0) >> 63)
	maxint  = int(^uint(1 << (bitsize - 1)))
)

func TestControlWriter(t *testing.T) {
	const (
		server = ws.StateServerSide
		client = ws.StateClientSide
	)
	for _, test := range []struct {
		name  string
		size  int
		write []byte
		state ws.State
		op    ws.OpCode
		exp   ws.Frame
		err   bool
	}{
		{
			state: server,
			op:    ws.OpPing,
			exp:   ws.NewPingFrame(nil),
		},
		{
			write: []byte("0123456789"),
			state: server,
			op:    ws.OpPing,
			exp:   ws.NewPingFrame([]byte("0123456789")),
		},
		{
			size:  10 + reserve(server, 10),
			write: []byte("0123456789"),
			state: server,
			op:    ws.OpPing,
			exp:   ws.NewPingFrame([]byte("0123456789")),
		},
		{
			size:  10 + reserve(server, 10),
			write: []byte("0123456789a"),
			state: server,
			op:    ws.OpPing,
			err:   true,
		},
		{
			write: bytes.Repeat([]byte{'x'}, ws.MaxControlFramePayloadSize+1),
			state: server,
			op:    ws.OpPing,
			err:   true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			var w *ControlWriter
			if n := test.size; n == 0 {
				w = NewControlWriter(&buf, test.state, test.op)
			} else {
				p := make([]byte, n)
				w = NewControlWriterBuffer(&buf, test.state, test.op, p)
			}

			_, err := w.Write(test.write)
			if err == nil {
				err = w.Flush()
			}
			if test.err {
				if err == nil {
					t.Errorf("want error")
				}
				return
			}
			if !test.err && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			act, err := ws.ReadFrame(&buf)
			if err != nil {
				t.Fatal(err)
			}

			act = omitMask(act)
			exp := omitMask(test.exp)
			if !reflect.DeepEqual(act, exp) {
				t.Errorf("unexpected frame:\nflushed: %v\nwant: %v", pretty(act), pretty(exp))
			}
		})
	}
}

type reserveTestCase struct {
	name      string
	buf       int
	state     ws.State
	expOffset int
	panic     bool
}

func genReserveTestCases(s ws.State, n, m, exp int) []reserveTestCase {
	ret := make([]reserveTestCase, m-n)
	for i := n; i < m; i++ {
		var suffix string
		if s.ClientSide() {
			suffix = " masked"
		}

		ret[i-n] = reserveTestCase{
			name:      "gen " + strconv.Itoa(i) + suffix,
			buf:       i,
			state:     s,
			expOffset: exp,
		}
	}
	return ret
}

func fakeMake(n int) (r []byte) {
	rh := (*reflect.SliceHeader)(unsafe.Pointer(&r))
	*rh = reflect.SliceHeader{
		Len: n,
		Cap: n,
	}
	return r
}

var reserveTestCases = []reserveTestCase{
	{
		name:      "len7",
		buf:       int(len7) + 2,
		expOffset: 2,
	},
	{
		name:      "len16",
		buf:       int(len16) + 4,
		expOffset: 4,
	},
	{
		name:      "maxint",
		buf:       maxint,
		expOffset: 10,
	},
	{
		name:      "len7 masked",
		buf:       int(len7) + 6,
		state:     ws.StateClientSide,
		expOffset: 6,
	},
	{
		name:      "len16 masked",
		buf:       int(len16) + 8,
		state:     ws.StateClientSide,
		expOffset: 8,
	},
	{
		name:      "maxint masked",
		buf:       maxint,
		state:     ws.StateClientSide,
		expOffset: 14,
	},
	{
		name:      "split case",
		buf:       128,
		expOffset: 4,
	},
}

func TestNewWriterBuffer(t *testing.T) {
	cases := append(
		reserveTestCases,
		reserveTestCase{
			name:  "panic",
			buf:   2,
			panic: true,
		},
		reserveTestCase{
			name:  "panic",
			buf:   6,
			state: ws.StateClientSide,
			panic: true,
		},
	)
	cases = append(cases, genReserveTestCases(0, int(len7)-2, int(len7)+2, 2)...)
	cases = append(cases, genReserveTestCases(0, int(len16)-4, int(len16)+4, 4)...)
	cases = append(cases, genReserveTestCases(0, maxint-10, maxint, 10)...)

	cases = append(cases, genReserveTestCases(ws.StateClientSide, int(len7)-6, int(len7)+6, 6)...)
	cases = append(cases, genReserveTestCases(ws.StateClientSide, int(len16)-8, int(len16)+8, 8)...)
	cases = append(cases, genReserveTestCases(ws.StateClientSide, maxint-14, maxint, 14)...)

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				thePanic := recover()
				if test.panic && thePanic == nil {
					t.Errorf("expected panic")
				}
				if !test.panic && thePanic != nil {
					t.Errorf("unexpected panic: %v", thePanic)
				}
			}()
			w := NewWriterBuffer(nil, test.state, 0, fakeMake(test.buf))
			if act, exp := len(w.raw)-len(w.buf), test.expOffset; act != exp {
				t.Errorf(
					"NewWriteBuffer(%d bytes) has offset %d; want %d",
					test.buf, act, exp,
				)
			}
		})
	}
}

func TestWriter(t *testing.T) {
	for i, test := range []struct {
		label  string
		size   int
		state  ws.State
		data   [][]byte
		expFrm []ws.Frame
		expBts []byte
	}{
		// No Write(), no frames.
		{},

		{
			data: [][]byte{
				{},
			},
			expBts: ws.MustCompileFrame(ws.NewTextFrame(nil)),
		},
		{
			data: [][]byte{
				[]byte("hello, world!"),
			},
			expBts: ws.MustCompileFrame(ws.NewTextFrame([]byte("hello, world!"))),
		},
		{
			state: ws.StateClientSide,
			data: [][]byte{
				[]byte("hello, world!"),
			},
			expFrm: []ws.Frame{ws.MaskFrame(ws.NewTextFrame([]byte("hello, world!")))},
		},
		{
			size: 5,
			data: [][]byte{
				[]byte("hello"),
				[]byte(", wor"),
				[]byte("ld!"),
			},
			expBts: bytes.Join(
				bts(
					ws.MustCompileFrame(ws.Frame{
						Header: ws.Header{
							Fin:    false,
							OpCode: ws.OpText,
							Length: 5,
						},
						Payload: []byte("hello"),
					}),
					ws.MustCompileFrame(ws.Frame{
						Header: ws.Header{
							Fin:    false,
							OpCode: ws.OpContinuation,
							Length: 5,
						},
						Payload: []byte(", wor"),
					}),
					ws.MustCompileFrame(ws.Frame{
						Header: ws.Header{
							Fin:    true,
							OpCode: ws.OpContinuation,
							Length: 3,
						},
						Payload: []byte("ld!"),
					}),
				),
				nil,
			),
		},
		{ // Large write case.
			size: 5,
			data: [][]byte{
				[]byte("hello, world!"),
			},
			expBts: bytes.Join(
				bts(
					ws.MustCompileFrame(ws.Frame{
						Header: ws.Header{
							Fin:    false,
							OpCode: ws.OpText,
							Length: 13,
						},
						Payload: []byte("hello, world!"),
					}),
					ws.MustCompileFrame(ws.Frame{
						Header: ws.Header{
							Fin:    true,
							OpCode: ws.OpContinuation,
							Length: 0,
						},
					}),
				),
				nil,
			),
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			buf := &bytes.Buffer{}
			w := NewWriterSize(buf, test.state, ws.OpText, test.size)

			for _, p := range test.data {
				_, err := w.Write(p)
				if err != nil {
					t.Fatalf("unexpected Write() error: %s", err)
				}
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("unexpected Flush() error: %s", err)
			}
			if test.expBts != nil {
				if bts := buf.Bytes(); !bytes.Equal(test.expBts, bts) {
					t.Errorf(
						"wrote bytes:\nact:\t%#x\nexp:\t%#x\nacth:\t%s\nexph:\t%s\n", bts, test.expBts,
						pretty(frames(bts)...), pretty(frames(test.expBts)...),
					)
				}
			}
			if test.expFrm != nil {
				act := omitMasks(frames(buf.Bytes()))
				exp := omitMasks(test.expFrm)

				if !reflect.DeepEqual(act, exp) {
					t.Errorf(
						"wrote frames (mask omitted):\nact:\t%s\nexp:\t%s\n",
						pretty(act...), pretty(exp...),
					)
				}
			}
		})
	}
}

func TestWriterLargeWrite(t *testing.T) {
	var dest bytes.Buffer
	w := NewWriterSize(&dest, 0, 0, 16)

	// Test that even for big writes extensions set their bits.
	rsv := [3]bool{true, true, false}
	w.SetExtensions(SendExtensionFunc(func(h ws.Header) (ws.Header, error) {
		h.Rsv = ws.Rsv(rsv[0], rsv[1], rsv[2])
		return h, nil
	}))

	// Write message with size twice bigger than writer's internal buffer.
	// We expect Writer to write it directly without buffering since we didn't
	// write anything before (no data in internal buffer).
	bts := make([]byte, 2*w.Size())
	if _, err := w.Write(bts); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	frame, err := ws.ReadFrame(&dest)
	if err != nil {
		t.Fatalf("can't read frame: %v", err)
	}

	var act [3]bool
	act[0], act[1], act[2] = ws.RsvBits(frame.Header.Rsv)
	if act != rsv {
		t.Fatalf("unexpected rsv bits sent: %v; extension set %v", act, rsv)
	}
}

func TestWriterGrow(t *testing.T) {
	for _, test := range []struct {
		name     string
		dataSize int
		numWrite int
	}{
		{
			name:     "buffer grow leads to its reduce",
			dataSize: 20,
		},
		{
			name:     "header size increases",
			dataSize: int(len16) + 10,
		},
		{
			name:     "split case for header offset",
			dataSize: int(len7),
		},
		{
			name:     "calculate header size from the payload instead of the whole buffer",
			dataSize: int(len7/2 + 2),
			numWrite: 2,
		},
		{
			name:     "shift current buffer when header size increase",
			dataSize: int(len7 - 2),
			numWrite: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var dest bytes.Buffer
			w := NewWriterSize(&dest, 0, 0, 16)
			w.DisableFlush()

			// Test that even for big writes extensions set their bits.
			rsv := [3]bool{true, true, false}
			w.SetExtensions(SendExtensionFunc(func(h ws.Header) (ws.Header, error) {
				h.Rsv = ws.Rsv(rsv[0], rsv[1], rsv[2])
				return h, nil
			}))

			bts := make([]byte, test.dataSize)
			if _, err := rand.Read(bts); err != nil {
				t.Fatal(err)
			}
			if test.numWrite == 0 {
				test.numWrite = 1
			}
			err := chunks(bts, test.numWrite, func(p []byte) error {
				_, err := w.Write(p)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}

			frame, err := ws.ReadFrame(&dest)
			if err != nil {
				t.Fatalf("can't read frame: %v", err)
			}
			var act [3]bool
			act[0], act[1], act[2] = ws.RsvBits(frame.Header.Rsv)
			if act != rsv {
				t.Fatalf("unexpected rsv bits sent: %v; extension set %v", act, rsv)
			}
			if !bytes.Equal(frame.Payload, bts) {
				t.Errorf("wrote frames:\nact:\t%x\nexp:\t%x\n", frame.Payload, bts)
			}
		})
	}
}

func TestWriterReadFrom(t *testing.T) {
	for i, test := range []struct {
		label string
		chop  int
		size  int
		data  []byte
		exp   []ws.Frame
		n     int64
	}{
		{
			chop: 1,
			size: 1,
			data: []byte("golang"),
			exp: []ws.Frame{
				{Header: ws.Header{Fin: false, Length: 1, OpCode: ws.OpText}, Payload: []byte{'g'}},
				{Header: ws.Header{Fin: false, Length: 1, OpCode: ws.OpContinuation}, Payload: []byte{'o'}},
				{Header: ws.Header{Fin: false, Length: 1, OpCode: ws.OpContinuation}, Payload: []byte{'l'}},
				{Header: ws.Header{Fin: false, Length: 1, OpCode: ws.OpContinuation}, Payload: []byte{'a'}},
				{Header: ws.Header{Fin: false, Length: 1, OpCode: ws.OpContinuation}, Payload: []byte{'n'}},
				{Header: ws.Header{Fin: false, Length: 1, OpCode: ws.OpContinuation}, Payload: []byte{'g'}},
				{Header: ws.Header{Fin: true, Length: 0, OpCode: ws.OpContinuation}},
			},
			n: 6,
		},
		{
			chop: 1,
			size: 4,
			data: []byte("golang"),
			exp: []ws.Frame{
				{Header: ws.Header{Fin: false, Length: 4, OpCode: ws.OpText}, Payload: []byte("gola")},
				{Header: ws.Header{Fin: true, Length: 2, OpCode: ws.OpContinuation}, Payload: []byte("ng")},
			},
			n: 6,
		},
		{
			size: 64,
			data: []byte{},
			exp: []ws.Frame{
				{Header: ws.Header{Fin: true, Length: 0, OpCode: ws.OpText}},
			},
			n: 0,
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			dst := &bytes.Buffer{}
			wr := NewWriterSize(dst, 0, ws.OpText, test.size)

			chop := test.chop
			if chop == 0 {
				chop = 128
			}
			src := &chopReader{bytes.NewReader(test.data), chop}

			n, err := wr.ReadFrom(src)
			if err == nil {
				err = wr.Flush()
			}
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if n != test.n {
				t.Errorf("ReadFrom() read out %d; want %d", n, test.n)
			}
			if frames := frames(dst.Bytes()); !reflect.DeepEqual(frames, test.exp) {
				t.Errorf("ReadFrom() read frames:\n\tact:\t%s\n\texp:\t%s\n", pretty(frames...), pretty(test.exp...))
			}
		})
	}
}

func TestWriterWriteCount(t *testing.T) {
	for _, test := range []struct {
		name  string
		cap   int
		exp   int
		write []int // For ability to avoid large write inside Write()'s "if".
	}{
		{
			name:  "one frame",
			cap:   10,
			write: []int{10},
			exp:   1,
		},
		{
			name:  "two frames",
			cap:   10,
			write: []int{5, 7},
			exp:   2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			n := writeCounter{}
			w := NewWriterSize(&n, 0, ws.OpText, test.cap)

			for _, n := range test.write {
				text := bytes.Repeat([]byte{'x'}, n)
				if _, err := w.Write(text); err != nil {
					t.Fatal(err)
				}
			}

			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}

			if act, exp := n.n, test.exp; act != exp {
				t.Errorf("made %d Write() calls to dest writer; want %d", act, exp)
			}
		})
	}
}

func TestWriterNoPreemtiveFlush(t *testing.T) {
	n := writeCounter{}
	w := NewWriterSize(&n, 0, 0, 10)

	// Fill buffer.
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if n.n != 0 {
		t.Fatalf(
			"after filling up Writer got %d writes to the dest; want 0",
			n.n,
		)
	}
}

type writeCounter struct {
	n int
}

func (w *writeCounter) Write(p []byte) (int, error) {
	w.n++
	return len(p), nil
}

func frames(p []byte) (ret []ws.Frame) {
	r := bytes.NewReader(p)
	for stop := false; !stop; {
		f, err := ws.ReadFrame(r)
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}
		ret = append(ret, f)
	}
	return ret
}

func pretty(f ...ws.Frame) string {
	str := "\n"
	for _, f := range f {
		str += fmt.Sprintf("\t%#v\n\t%#x (%#q)\n\t----\n", f.Header, f.Payload, f.Payload)
	}
	return str
}

func omitMask(f ws.Frame) ws.Frame {
	if f.Header.Masked {
		p := make([]byte, int(f.Header.Length))
		copy(p, f.Payload)

		ws.Cipher(p, f.Header.Mask, 0)

		f.Header.Mask = [4]byte{0, 0, 0, 0}
		f.Payload = p
	}
	return f
}

func omitMasks(f []ws.Frame) []ws.Frame {
	for i := 0; i < len(f); i++ {
		f[i] = omitMask(f[i])
	}
	return f
}

func bts(b ...[]byte) [][]byte { return b }

func chunks(p []byte, n int, fn func(p []byte) error) error {
	if len(p) < n {
		panic("buffer is smaller than requested number of chunks")
	}
	step := len(p) / n
	for pos, i := 0, 0; i < len(p)/step; pos, i = pos+step, i+1 {
		if i == n-1 {
			// Last iteration.
			step += len(p) % n
		}
		if err := fn(p[pos : pos+step]); err != nil {
			return err
		}
	}
	return nil
}
