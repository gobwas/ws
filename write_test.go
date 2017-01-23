package ws

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"
)

func TestWriteHeader(t *testing.T) {
	for i, test := range RWCases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteHeader(buf, test.Header)
			if test.Err && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !test.Err && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.Err {
				return
			}
			if bts := buf.Bytes(); !bytes.Equal(bts, test.Data) {
				t.Errorf("WriteHeader()\nwrote:\n\t%08b\nwant:\n\t%08b", bts, test.Data)
			}
		})
	}
}

func BenchmarkWriteHeader(b *testing.B) {
	for _, bench := range []struct {
		label  string
		header Header
	}{
		{
			"ping", Header{
				OpCode: OpPing,
				Fin:    true,
			},
		},
		{
			"text16", Header{
				OpCode: OpText,
				Fin:    true,
				Length: int64(^(uint16(0))),
			},
		},
		{
			"text64", Header{
				OpCode: OpText,
				Fin:    true,
				Length: int64(^(uint64(0)) >> 1),
			},
		},
		{
			"text64mask", Header{
				OpCode: OpText,
				Fin:    true,
				Length: int64(^(uint64(0)) >> 1),
				Mask:   [4]byte{'m', 'a', 's', 'k'},
			},
		},
	} {
		b.Run(bench.label, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if err := WriteHeader(ioutil.Discard, bench.header); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
