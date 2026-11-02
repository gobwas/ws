//go:build !purego

package ws

import (
	"fmt"
	"testing"
)

// TestCipherKernels runs the Cipher tests with each amd64 kernel forced, so
// that both are exercised whatever the test machine supports.
func TestCipherKernels(t *testing.T) {
	defer func(v bool) { useAVX2 = v }(useAVX2)

	t.Run("sse2", func(t *testing.T) {
		useAVX2 = false
		TestCipher(t)
		TestCipherLarge(t)
	})
	if !cpuHasAVX2() {
		t.Log("no AVX2 on this machine; AVX2 kernel not run")
		return
	}
	t.Run("avx2", func(t *testing.T) {
		useAVX2 = true
		TestCipher(t)
		TestCipherLarge(t)
	})
}

// BenchmarkCipherKernels measures each amd64 kernel on its own, whatever the
// machine would pick, so that the two can be compared on one run.
func BenchmarkCipherKernels(b *testing.B) {
	defer func(v bool) { useAVX2 = v }(useAVX2)

	kernels := []struct {
		name string
		avx2 bool
	}{
		{
			name: "sse2",
			avx2: false,
		},
		{
			name: "avx2",
			avx2: true,
		},
	}
	for _, k := range kernels {
		if k.avx2 && !cpuHasAVX2() {
			continue
		}
		useAVX2 = k.avx2
		for _, size := range []int{125, 1024, 4096, 32768} {
			bts := make([]byte, size)
			var mask [4]byte
			mask[1] = 0xaa
			b.Run(fmt.Sprintf("%s/bytes=%d", k.name, size), func(b *testing.B) {
				b.SetBytes(int64(size))
				for b.Loop() {
					Cipher(bts, mask, 3)
				}
			})
		}
	}
}
