package ws

import "testing"

func BenchmarkInitAcceptFromNonce(b *testing.B) {
	dst := make([]byte, acceptSize)
	nonce := mustMakeNonce()
	for i := 0; i < b.N; i++ {
		initAcceptFromNonce(dst, nonce)
	}
}
