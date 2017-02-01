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
	{"websocket", "WebSocket", "websocket"},
	{"upgrade", "Upgrade", "upgrade"},

	randomEqualLetters(20),
	randomEqualLetters(24),
	randomEqualLetters(64),

	inequalAt(randomEqualLetters(20), 10),
	inequalAt(randomEqualLetters(24), 17),
	inequalAt(randomEqualLetters(64), 30),
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
