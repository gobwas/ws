package ws

import (
	"encoding/base64"
	"testing"
)

func TestCheckNonce(t *testing.T) {
	// DecodedLen reports the same upper bound for all four inputs.
	// Only decoding distinguishes invalid base64 and the actual key length.
	for _, test := range []struct {
		nonce string
		want  bool
	}{
		{
			nonce: base64.StdEncoding.EncodeToString(make([]byte, 16)),
			want:  true,
		},
		{
			nonce: "!!!!!!!!!!!!!!!!!!!!!!!!",
		},
		{
			nonce: base64.StdEncoding.EncodeToString(make([]byte, 17)),
		},
		{
			nonce: base64.StdEncoding.EncodeToString(make([]byte, 18)),
		},
	} {
		if got := checkNonce([]byte(test.nonce)); got != test.want {
			t.Errorf(
				"checkNonce(%q) = %t, want %t",
				test.nonce, got, test.want,
			)
		}
	}
}

func BenchmarkInitAcceptFromNonce(b *testing.B) {
	dst := make([]byte, acceptSize)
	nonce := mustMakeNonce()
	for b.Loop() {
		initAcceptFromNonce(dst, nonce)
	}
}
