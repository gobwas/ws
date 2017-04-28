package ws

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/textproto"
	"strings"
	"testing"
)

var compareWithStd = flag.Bool("std", false, "compare with standard library implementation (if exists)")

var readLineCases = []struct {
	label   string
	in      string
	line    []byte
	err     error
	bufSize int
}{
	{
		label:   "simple",
		in:      "hello, world!",
		line:    []byte("hello, world!"),
		err:     io.EOF,
		bufSize: 1024,
	},
	{
		label:   "simple",
		in:      "hello, world!\r\n",
		line:    []byte("hello, world!"),
		bufSize: 1024,
	},
	{
		label:   "simple",
		in:      "hello, world!\n",
		line:    []byte("hello, world!"),
		bufSize: 1024,
	},
	{
		// The case where "\r\n" straddles the buffer.
		label:   "straddle",
		in:      "hello, world!!!\r\n...",
		line:    []byte("hello, world!!!"),
		bufSize: 16,
	},
	{
		label:   "chunked",
		in:      "hello, world! this is a long long line!",
		line:    []byte("hello, world! this is a long long line!"),
		err:     io.EOF,
		bufSize: 16,
	},
	{
		label:   "chunked",
		in:      "hello, world! this is a long long line!\r\n",
		line:    []byte("hello, world! this is a long long line!"),
		bufSize: 16,
	},
}

func TestReadLine(t *testing.T) {
	for _, test := range readLineCases {
		t.Run(test.label, func(t *testing.T) {
			br := bufio.NewReaderSize(strings.NewReader(test.in), test.bufSize)
			bts, err := readLine(br)
			if err != test.err {
				t.Errorf("unexpected error: %v; want %v", err, test.err)
			}
			if act, exp := bts, test.line; !bytes.Equal(act, exp) {
				t.Errorf("readLine() result is %#q; want %#q", act, exp)
			}
		})
	}
}

func BenchmarkReadLine(b *testing.B) {
	for _, test := range readLineCases {
		sr := strings.NewReader(test.in)
		br := bufio.NewReaderSize(sr, test.bufSize)
		b.Run(test.label, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = readLine(br)
				sr.Reset(test.in)
				br.Reset(sr)
			}
		})
	}
}

func TestHasToken(t *testing.T) {
	for i, test := range []struct {
		header string
		token  string
		exp    bool
	}{
		{"Keep-Alive, Close, Upgrade", "upgrade", true},
		{"Keep-Alive, Close, upgrade, hello", "upgrade", true},
		{"Keep-Alive, Close,  hello", "upgrade", false},
	} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			if has := strHasToken(test.header, test.token); has != test.exp {
				t.Errorf("hasToken(%q, %q) = %v; want %v", test.header, test.token, has, test.exp)
			}
		})
	}
}

func BenchmarkHasToken(b *testing.B) {
	for i, bench := range []struct {
		header string
		token  string
	}{
		{"Keep-Alive, Close, Upgrade", "upgrade"},
		{"Keep-Alive, Close, upgrade, hello", "upgrade"},
		{"Keep-Alive, Close,  hello", "upgrade"},
	} {
		b.Run(fmt.Sprintf("#%d", i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = strHasToken(bench.header, bench.token)
			}
		})
	}
}

type equalFoldCase struct {
	label string
	a, b  string
}

var equalFoldCases = []equalFoldCase{
	{"websocket", "WebSocket", "websocket"},
	{"upgrade", "Upgrade", "upgrade"},
	randomEqualLetters(512),
	inequalAt(randomEqualLetters(512), 256),
}

func TestStrEqualFold(t *testing.T) {
	for i, test := range equalFoldCases {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			if len(test.a) < 100 && len(test.b) < 100 {
				t.Logf("\n\ta: %s\n\tb: %s\n", test.a, test.b)
			}
			exp := strings.EqualFold(test.a, test.b)
			if act := strEqualFold(test.a, test.b); act != exp {
				t.Errorf("strEqualFold(%q, %q) = %v; want %v", test.a, test.b, act, exp)
			}
		})
	}
}

func BenchmarkStrEqualFold(b *testing.B) {
	for i, bench := range equalFoldCases {
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = strEqualFold(bench.a, bench.b)
			}
		})
	}
	if *compareWithStd {
		for i, bench := range equalFoldCases {
			b.Run(fmt.Sprintf("%s#%d_std", bench.label, i), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = strings.EqualFold(bench.a, bench.b)
				}
			})
		}
	}
}

func BenchmarkBtsEqualFold(b *testing.B) {
	for i, bench := range equalFoldCases {
		ab, bb := []byte(bench.a), []byte(bench.b)
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = btsEqualFold(ab, bb)
			}
		})
	}
	if *compareWithStd {
		for i, bench := range equalFoldCases {
			ab, bb := []byte(bench.a), []byte(bench.b)
			b.Run(fmt.Sprintf("%s#%d_std", bench.label, i), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = bytes.EqualFold(ab, bb)
				}
			})
		}
	}
}

func TestAsciiToInt(t *testing.T) {
	for _, test := range []struct {
		bts []byte
		exp int
		err bool
	}{
		{[]byte{'0'}, 0, false},
		{[]byte{'1'}, 1, false},
		{[]byte("42"), 42, false},
		{[]byte("420"), 420, false},
		{[]byte("010050042"), 10050042, false},
	} {
		t.Run(fmt.Sprintf("%s", string(test.bts)), func(t *testing.T) {
			act, err := asciiToInt(test.bts)
			if (test.err && err == nil) || (!test.err && err != nil) {
				t.Errorf("unexpected error: %v", err)
			}
			if act != test.exp {
				t.Errorf("asciiToInt(%v) = %v; want %v", test.bts, act, test.exp)
			}
		})
	}
}

func TestBtrim(t *testing.T) {
	for _, test := range []struct {
		bts []byte
		exp []byte
	}{
		{[]byte("abc"), []byte("abc")},
		{[]byte(" abc"), []byte("abc")},
		{[]byte("abc "), []byte("abc")},
		{[]byte(" abc "), []byte("abc")},
	} {
		t.Run(fmt.Sprintf("%s", string(test.bts)), func(t *testing.T) {
			if act := btrim(test.bts); !bytes.Equal(act, test.exp) {
				t.Errorf("btrim(%v) = %v; want %v", test.bts, act, test.exp)
			}
		})
	}
}

func TestBSplit3(t *testing.T) {
	for _, test := range []struct {
		bts  []byte
		sep  byte
		exp1 []byte
		exp2 []byte
		exp3 []byte
	}{
		{[]byte(""), ' ', []byte{}, nil, nil},
		{[]byte("GET / HTTP/1.1"), ' ', []byte("GET"), []byte("/"), []byte("HTTP/1.1")},
	} {
		t.Run(fmt.Sprintf("%s", string(test.bts)), func(t *testing.T) {
			b1, b2, b3 := bsplit3(test.bts, test.sep)
			if !bytes.Equal(b1, test.exp1) || !bytes.Equal(b2, test.exp2) || !bytes.Equal(b3, test.exp3) {
				t.Errorf(
					"bsplit3(%q) = %q, %q, %q; want %q, %q, %q",
					string(test.bts), string(b1), string(b2), string(b3),
					string(test.exp1), string(test.exp2), string(test.exp3),
				)
			}
		})
	}
}

var canonicalHeaderCases = [][]byte{
	[]byte("foo-"),
	[]byte("-foo"),
	[]byte("-"),
	[]byte("foo----bar"),
	[]byte("foo-bar"),
	[]byte("FoO-BaR"),
	[]byte("Foo-Bar"),
	[]byte("sec-websocket-extensions"),
}

func TestCanonicalizeHeaderKey(t *testing.T) {
	for _, bts := range canonicalHeaderCases {
		t.Run(fmt.Sprintf("%s", string(bts)), func(t *testing.T) {
			act := append([]byte(nil), bts...)
			canonicalizeHeaderKey(act)

			exp := strToBytes(textproto.CanonicalMIMEHeaderKey(string(bts)))

			if !bytes.Equal(act, exp) {
				t.Errorf(
					"canonicalizeHeaderKey(%v) = %v; want %v",
					string(bts), string(act), string(exp),
				)
			}
		})
	}
}

func BenchmarkCanonicalizeHeaderKey(b *testing.B) {
	for _, bts := range canonicalHeaderCases {
		b.Run(fmt.Sprintf("%s", string(bts)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				canonicalizeHeaderKey(bts)
			}
		})
	}
}

func randomEqualLetters(n int) (c equalFoldCase) {
	c.label = fmt.Sprintf("rnd_eq_%d", n)

	a, b := make([]byte, n), make([]byte, n)

	for i := 0; i < n; i++ {
		c := byte(rand.Intn('Z'-'A'+1) + 'A') // Random character from 'A' to 'Z'.
		a[i] = c
		b[i] = c | ('a' - 'A') // Swap fold.
	}

	c.a = string(a)
	c.b = string(b)

	return
}

func inequalAt(c equalFoldCase, i int) equalFoldCase {
	bts := make([]byte, len(c.a))
	copy(bts, c.a)
	for {
		b := byte(rand.Intn('z'-'a'+1) + 'a')
		if bts[i] != b {
			bts[i] = b
			c.a = string(bts)
			c.label = fmt.Sprintf("rnd_ineq_%d_%d", len(c.a), i)
			return c
		}
	}
}
