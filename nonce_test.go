package ws

import "testing"

func BenchmarkInitAcceptFromNonce(b *testing.B) {
	dst := make([]byte, acceptSize)
	nonce := mustMakeNonce()
	for b.Loop() {
		initAcceptFromNonce(dst, nonce)
	}
}
