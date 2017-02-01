package ws

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

type equalFoldCase struct {
	label string
	a, b  string
}

var equalFoldCases = []equalFoldCase{
	{"simple_case", "websocket", "WebSocket"},
	randomEqual(20),
	randomEqual(24),
	randomEqual(1000),
	randomEqual(1024),
}

func TestEqualFold(t *testing.T) {
	for i, test := range equalFoldCases {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			if len(test.a) < 100 && len(test.b) < 100 {
				t.Logf("\n\ta: %s\n\tb: %s\n", test.a, test.b)
			}
			exp := strings.EqualFold(test.a, test.b)
			if act := equalFold(test.a, test.b); act != exp {
				t.Errorf("equalFold(%q, %q) = %v; want %v", test.a, test.b, act, exp)
			}
		})
	}
}

func BenchmarkEqualFold(b *testing.B) {
	for i, bench := range equalFoldCases {
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = equalFold(bench.a, bench.b)
			}
		})
		b.Run(fmt.Sprintf("%s#%d_str", bench.label, i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = strings.EqualFold(bench.a, bench.b)
			}
		})
	}
}

func randomEqual(n int) (c equalFoldCase) {
	c.label = fmt.Sprintf("random_eq_%d", n)

	a, b := make([]byte, n), make([]byte, n)

	for i := 0; i < n; i++ {
		c := byte(rand.Intn('~'-' '+1) + ' ') // Random character from '~' to ' '.

		a[i] = c

		if 'A' <= c && c <= 'Z' && rand.Intn(2) == 1 {
			b[i] = c | ('a' - 'A') // Swap fold.
		} else {
			b[i] = c
		}
	}

	c.a = string(a)
	c.b = string(b)

	return
}
