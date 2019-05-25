package ws

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"
)

type StackEatingReader struct {
	MaxDepth int
	Source   io.Reader
}

func (s StackEatingReader) Read(p []byte) (n int, err error) {
	var x [16]byte
	ptr := uintptr(unsafe.Pointer(&x))

	var f func(int)
	f = func(lim int) {
		if lim == 0 {
			err = fmt.Errorf("stack eating reader: not enough recursion depth")
			return
		}
		if act := uintptr(unsafe.Pointer(&x)); act != ptr {
			// Stack has been moved!
			n, err = s.Source.Read(p)
			return
		}
		f(lim - 1)
	}

	f(s.MaxDepth)

	return n, err
}

func makeTempFile(name string, p []byte) (f *os.File, err error) {
	f, err = ioutil.TempFile("", strings.ToLower(name))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
			f = nil
		}
	}()
	if _, err = f.Write(p); err != nil {
		return
	}
	if _, err = f.Seek(0, 0); err != nil {
		return
	}
	return
}

func TestReadHeaderStackMoveNetwork(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	var (
		client = make(chan net.Conn)
		server = make(chan net.Conn)
	)
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
			return
		}
		client <- conn
	}()
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
			return
		}
		server <- conn
	}()

	c := <-client
	s := <-server

	received := make(chan Header)
	go func() {
		r := StackEatingReader{
			MaxDepth: 1000,
			Source:   s,
		}
		h, err := ReadHeader(r)
		if err != nil {
			t.Fatalf("ReadHeader() error: %v", err)
			return
		}
		received <- h
	}()

	exp := Header{
		Fin:    true,
		OpCode: OpText,
		Masked: true,
		Mask:   [4]byte{1, 2, 3, 4},
		Length: 15,
	}
	time.AfterFunc(time.Second, func() {
		if err := WriteHeader(c, exp); err != nil {
			t.Fatalf("WriteHeader() error: %v", err)
		}
	})

	act := <-received
	if act != exp {
		t.Fatalf("ReadHeader() unexpected header: %+v; want %+v", act, exp)
	}
}

func TestReadHeaderStackMove(t *testing.T) {
	for _, test := range []struct {
		name string
		file bool
	}{
		{
			name: "in memory",
			file: false,
		},
		{
			name: "temp file",
			file: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Prepare bytes of header we expect to read.
			head := bits("1 000 0001 1 0001111 00000001 00000010 00000011 00000100")
			//            _ ___ ____ _ _______ ___________________________________
			//            |  |   |   |    |                     |
			//           Fin |   |  Mask Length                Mask
			//              Rsv  |
			//                 OpCode
			exp := Header{
				Fin:    true,
				OpCode: OpText,
				Masked: true,
				Mask:   [4]byte{1, 2, 3, 4},
				Length: 15,
			}

			var source io.Reader
			if test.file {
				name := strings.Replace(t.Name(), "/", "", -1)
				f, err := makeTempFile(name, head)
				if err != nil {
					t.Fatal(err)
				}
				defer os.Remove(f.Name())
				source = f
			} else {
				source = bytes.NewReader(head)
			}

			r := StackEatingReader{
				MaxDepth: 1000,
				Source:   source,
			}
			act, err := ReadHeader(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if act != exp {
				t.Fatalf("ReadHeader() unexpected header: %+v; want %+v", act, exp)
			}
		})
	}
}

func TestReadHeader(t *testing.T) {
	for i, test := range append([]RWTestCase{
		{
			Data: bits("0000 0000 0 1111111 10000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000"),
			//                              _______________________________________________________________________
			//                                                                 |
			//                                                            Length value
			Err: true,
		},
	}, RWTestCases...) {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			r := bytes.NewReader(test.Data)
			h, err := ReadHeader(r)
			if test.Err && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !test.Err && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.Err {
				return
			}
			if !reflect.DeepEqual(h, test.Header) {
				t.Errorf("ReadHeader()\nread:\n\t%#v\nwant:\n\t%#v", h, test.Header)
			}
		})
	}
}

func BenchmarkReadHeader(b *testing.B) {
	for i, bench := range RWBenchCases {
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			bts := MustCompileFrame(Frame{Header: bench.header})
			rds := make([]io.Reader, b.N)
			for i := 0; i < b.N; i++ {
				rds[i] = bytes.NewReader(bts)
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := ReadHeader(rds[i])
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
