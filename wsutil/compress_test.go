package wsutil

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"testing"
)

func TestCompressWriter(t *testing.T) {
	for i, test := range []struct {
		label  string
		level  int
		data   []byte
		result []byte
	}{
		{
			label: "simple",
			level: 6,
			data:  []byte("hello world!"),
			result:  []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x28, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x04, 0x0},
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			buf := &bytes.Buffer{}
			cw, err := NewCompressWriter(buf, test.level)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
				return
			}
			_, err = cw.Write(test.data)
			if err != nil {
				t.Errorf("cannot write data: %s", err)
				return
			}
			if !reflect.DeepEqual(buf.Bytes(), test.result) {
				t.Errorf("write data is not equal:\n\tact:\t%#v\n\texp:\t%#v\n", buf.Bytes(), test.result)
				return
			}
		})
	}
}

func TestCompressReader(t *testing.T) {
	for i, test := range []struct {
		label string
		data  []byte
		result  []byte
		chop  int
	}{
		{
			label: "simple",
			data:  []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x28, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x04, 0x0},
			result:  []byte("hello world!"),
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			rd := bytes.NewReader(test.data)
			cr := NewCompressReader(rd)

			buf, err := ioutil.ReadAll(cr)
			if err != nil && err != io.EOF {
				t.Errorf("cannot read data: %s", err)
				return
			}

			if !reflect.DeepEqual(buf, test.result) {
				t.Errorf("write data is not equal:\n\tact:\t%#v\n\texp:\t%#v\n", buf, test.result)
				return
			}
		})
	}
}

func BenchmarkCompressWriter(b *testing.B) {
	for _, bench := range []struct {
		message  string
		repeated int
	}{
		{
			message: "hello world",
		},
		{
			message:  "hello world\n",
			repeated: 1000,
		},
		{
			message:  "hello world\n",
			repeated: 10000,
		},
	} {
		b.Run(fmt.Sprintf("message=%s;repeated=%d", bench.message, bench.repeated), func(b *testing.B) {
			buf := &bytes.Buffer{}
			for r := bench.repeated; r >= 0; r-- {
				buf.WriteString(bench.message)
			}

			for i := 0; i < b.N; i++ {
				cw, err := NewCompressWriter(ioutil.Discard, 6)
				if err != nil {
					b.Errorf("unexpected error: %s", err)
					return
				}

				_, err = cw.Write(buf.Bytes())
				if err != nil {
					b.Errorf("cannot write: %s", err)
					return
				}
			}
		})
	}
}