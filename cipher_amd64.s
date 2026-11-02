//go:build !purego

#include "textflag.h"

// func cipherBlocksSSE2(p []byte, key uint32)
//
// Xors 64 bytes per iteration, four 16-byte vectors against the key broadcast
// to every 32-bit lane, then the remaining 16-byte blocks one at a time. The
// caller guarantees len(p) is a multiple of 16.
TEXT ·cipherBlocksSSE2(SB), NOSPLIT, $0-28
	MOVQ   p_base+0(FP), AX
	MOVQ   p_len+8(FP), BX
	MOVL   key+24(FP), DX
	MOVD   DX, X4
	PSHUFD $0, X4, X4
	MOVQ   BX, CX
	SHRQ   $6, CX
	JZ     tail

loop64:
	MOVOU (AX), X0
	MOVOU 16(AX), X1
	MOVOU 32(AX), X2
	MOVOU 48(AX), X3
	PXOR  X4, X0
	PXOR  X4, X1
	PXOR  X4, X2
	PXOR  X4, X3
	MOVOU X0, (AX)
	MOVOU X1, 16(AX)
	MOVOU X2, 32(AX)
	MOVOU X3, 48(AX)
	ADDQ  $64, AX
	DECQ  CX
	JNZ   loop64

tail:
	ANDQ $63, BX
	JZ   done

loop16:
	MOVOU (AX), X0
	PXOR  X4, X0
	MOVOU X0, (AX)
	ADDQ  $16, AX
	SUBQ  $16, BX
	JNZ   loop16

done:
	RET

// func cipherBlocksAVX2(p []byte, key uint32)
//
// Same as above with two 32-byte vectors per iteration; the 16-byte remainder
// uses the low half of the broadcast register.
TEXT ·cipherBlocksAVX2(SB), NOSPLIT, $0-28
	MOVQ         p_base+0(FP), AX
	MOVQ         p_len+8(FP), BX
	MOVL         key+24(FP), DX
	MOVD         DX, X2
	VPBROADCASTD X2, Y2
	MOVQ         BX, CX
	SHRQ         $6, CX
	JZ           tail

loop64:
	VMOVDQU (AX), Y0
	VMOVDQU 32(AX), Y1
	VPXOR   Y2, Y0, Y0
	VPXOR   Y2, Y1, Y1
	VMOVDQU Y0, (AX)
	VMOVDQU Y1, 32(AX)
	ADDQ    $64, AX
	DECQ    CX
	JNZ     loop64

tail:
	ANDQ $63, BX
	JZ   done

loop16:
	VMOVDQU (AX), X0
	VPXOR   X2, X0, X0
	VMOVDQU X0, (AX)
	ADDQ    $16, AX
	SUBQ    $16, BX
	JNZ     loop16

done:
	VZEROUPPER
	RET

// func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL  eaxArg+0(FP), AX
	MOVL  ecxArg+4(FP), CX
	CPUID
	MOVL  AX, eax+8(FP)
	MOVL  BX, ebx+12(FP)
	MOVL  CX, ecx+16(FP)
	MOVL  DX, edx+20(FP)
	RET

// func xgetbv() (eax, edx uint32)
TEXT ·xgetbv(SB), NOSPLIT, $0-8
	MOVL   $0, CX
	XGETBV
	MOVL   AX, eax+0(FP)
	MOVL   DX, edx+4(FP)
	RET
