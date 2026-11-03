//go:build !purego

#include "textflag.h"

// func cipherBlocks(p []byte, key uint32)
//
// Xors 64 bytes per iteration, four 16-byte vectors against the key broadcast
// to every 32-bit lane, then the remaining 16-byte blocks one at a time. The
// caller guarantees len(p) is a multiple of 16.
TEXT ·cipherBlocks(SB), NOSPLIT, $0-28
	MOVD  p_base+0(FP), R0
	MOVD  p_len+8(FP), R1
	MOVWU key+24(FP), R2
	VDUP  R2, V4.S4
	LSR   $6, R1, R3
	CBZ   R3, tail

loop64:
	VLD1   (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VEOR   V4.B16, V0.B16, V0.B16
	VEOR   V4.B16, V1.B16, V1.B16
	VEOR   V4.B16, V2.B16, V2.B16
	VEOR   V4.B16, V3.B16, V3.B16
	VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R0)
	SUBS   $1, R3, R3
	BNE    loop64

tail:
	AND  $63, R1, R1
	CBZ  R1, done

loop16:
	VLD1   (R0), [V0.B16]
	VEOR   V4.B16, V0.B16, V0.B16
	VST1.P [V0.B16], 16(R0)
	SUBS   $16, R1, R1
	BNE    loop16

done:
	RET
