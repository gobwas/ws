package wsutil

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"testing"

	. "github.com/gobwas/ws"
)

func TestWriter(t *testing.T) {
	for i, test := range []struct {
		label  string
		size   int
		masked bool
		data   [][]byte
		expFrm []Frame
		expBts []byte
	}{
		{},
		{
			data: [][]byte{
				[]byte{},
			},
			expBts: MustCompileFrame(NewTextFrame("")),
		},
		{
			data: [][]byte{
				[]byte("hello, world!"),
			},
			expBts: MustCompileFrame(NewTextFrame("hello, world!")),
		},
		{
			masked: true,
			data: [][]byte{
				[]byte("hello, world!"),
			},
			expFrm: []Frame{MaskFrame(NewTextFrame("hello, world!"))},
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
					MustCompileFrame(Frame{
						Header: Header{
							Fin:    false,
							OpCode: OpText,
							Length: 5,
						},
						Payload: []byte("hello"),
					}),
					MustCompileFrame(Frame{
						Header: Header{
							Fin:    false,
							OpCode: OpContinuation,
							Length: 5,
						},
						Payload: []byte(", wor"),
					}),
					MustCompileFrame(Frame{
						Header: Header{
							Fin:    true,
							OpCode: OpContinuation,
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
					MustCompileFrame(Frame{
						Header: Header{
							Fin:    false,
							OpCode: OpText,
							Length: 13,
						},
						Payload: []byte("hello, world!"),
					}),
					MustCompileFrame(Frame{
						Header: Header{
							Fin:    true,
							OpCode: OpContinuation,
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
			w := NewWriterSize(buf, OpText, test.masked, test.size)

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
						pretty(frames(bts)), pretty(frames(test.expBts)),
					)
				}
			}
			if test.expFrm != nil {
				act := omitMask(frames(buf.Bytes()))
				exp := omitMask(test.expFrm)

				if !reflect.DeepEqual(act, exp) {
					t.Errorf(
						"wrote frames (mask omitted):\nact:\t%s\nexp:\t%s\n",
						pretty(act), pretty(exp),
					)
				}
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
		exp   []Frame
		n     int64
	}{
		{
			chop: 1,
			size: 1,
			data: []byte("golang"),
			exp: []Frame{
				Frame{Header: Header{Fin: false, Length: 1, OpCode: OpText}, Payload: []byte{'g'}},
				Frame{Header: Header{Fin: false, Length: 1, OpCode: OpContinuation}, Payload: []byte{'o'}},
				Frame{Header: Header{Fin: false, Length: 1, OpCode: OpContinuation}, Payload: []byte{'l'}},
				Frame{Header: Header{Fin: false, Length: 1, OpCode: OpContinuation}, Payload: []byte{'a'}},
				Frame{Header: Header{Fin: false, Length: 1, OpCode: OpContinuation}, Payload: []byte{'n'}},
				Frame{Header: Header{Fin: false, Length: 1, OpCode: OpContinuation}, Payload: []byte{'g'}},
				Frame{Header: Header{Fin: true, Length: 0, OpCode: OpContinuation}},
			},
			n: 6,
		},
		{
			chop: 1,
			size: 4,
			data: []byte("golang"),
			exp: []Frame{
				Frame{Header: Header{Fin: false, Length: 4, OpCode: OpText}, Payload: []byte("gola")},
				Frame{Header: Header{Fin: true, Length: 2, OpCode: OpContinuation}, Payload: []byte("ng")},
			},
			n: 6,
		},
		{
			size: 64,
			data: []byte{},
			exp: []Frame{
				Frame{Header: Header{Fin: true, Length: 0, OpCode: OpText}},
			},
			n: 0,
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			dst := &bytes.Buffer{}
			wr := NewWriterSize(dst, OpText, false, test.size)

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
				t.Errorf("ReadFrom() read frames:\n\tact:\t%s\n\texp:\t%s\n", pretty(frames), pretty(test.exp))
			}
		})
	}
}

func frames(p []byte) (ret []Frame) {
	r := bytes.NewReader(p)
	for stop := false; !stop; {
		f, err := ReadFrame(r)
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}
		ret = append(ret, f)
	}
	return
}

func pretty(f []Frame) string {
	str := "\n"
	for _, f := range f {
		str += fmt.Sprintf("\t%#v\n\t%#x (%s)\n\t----\n", f.Header, f.Payload, f.Payload)
	}
	return str
}

func omitMask(f []Frame) []Frame {
	for i := 0; i < len(f); i++ {
		if f[i].Header.Masked {
			Cipher(f[i].Payload, f[i].Header.Mask, 0)
			f[i].Header.Mask = [4]byte{0, 0, 0, 0}
		}
	}
	return f
}

func bts(b ...[]byte) [][]byte { return b }
