//go:build !purego

package ws

// cipherBlocks xors p with key repeated. len(p) must be a multiple of 16 and
// key must be the mask as a little-endian uint32, aligned to p[0].
//
// It uses AVX2 when the CPU and the OS support it and SSE2 otherwise, which
// every amd64 Go build may assume.
func cipherBlocks(p []byte, key uint32) {
	if useAVX2 {
		cipherBlocksAVX2(p, key)
	} else {
		cipherBlocksSSE2(p, key)
	}
}

var useAVX2 = cpuHasAVX2()

// cpuHasAVX2 reports whether AVX2 instructions can be used: the CPU has them
// and the OS saves the YMM registers on context switches.
//
// The standard library does the same in internal/cpu/cpu_x86.go, which is not
// importable from outside it.
func cpuHasAVX2() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return false
	}
	// This is the procedure from the Intel 64 and IA-32 Architectures Software
	// Developer's Manual, Volume 1, section 14.3 "Detection of AVX
	// instructions" (https://www.intel.com/sdm): check CPUID.1:ECX.OSXSAVE
	// (bit 27), check that XGETBV reports XCR0 bits 1 and 2 set (XMM and YMM
	// state are saved by the OS), check CPUID.1:ECX.AVX (bit 28), and for AVX2
	// itself CPUID.(EAX=7,ECX=0):EBX.AVX2 (bit 5). The bit positions are from
	// Volume 2A, the CPUID instruction.
	const (
		osxsave = 1 << 27     // CPUID.1:ECX
		avx     = 1 << 28     // CPUID.1:ECX
		avx2    = 1 << 5      // CPUID.7.0:EBX
		xmmYMM  = 1<<1 | 1<<2 // XCR0: XMM and YMM state enabled
	)
	_, _, ecx, _ := cpuid(1, 0)
	if ecx&osxsave == 0 || ecx&avx == 0 {
		return false
	}
	if xcr0, _ := xgetbv(); xcr0&xmmYMM != xmmYMM {
		return false
	}
	_, ebx, _, _ := cpuid(7, 0)
	return ebx&avx2 != 0
}

//go:noescape
func cipherBlocksSSE2(p []byte, key uint32)

//go:noescape
func cipherBlocksAVX2(p []byte, key uint32)

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

func xgetbv() (eax, edx uint32)
