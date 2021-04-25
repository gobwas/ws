package wsflate

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

func TestSuffixedReaderIface(t *testing.T) {
	for _, test := range []struct {
		src io.Reader
		exp bool
	}{
		{
			src: bytes.NewReader(nil),
			exp: true,
		},
		{
			src: io.TeeReader(nil, nil),
			exp: false,
		},
	} {
		t.Run(fmt.Sprintf("%T", test.src), func(t *testing.T) {
			isByteReader := func(r io.Reader) bool {
				_, ok := r.(io.ByteReader)
				return ok
			}
			s := &suffixedReader{
				r: test.src,
			}
			if act, exp := isByteReader(s.iface()), test.exp; act != exp {
				t.Fatalf("unexpected io.ByteReader: %t; want %t", act, exp)
			}
		})
	}
}
