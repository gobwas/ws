package ws

import (
	"reflect"
	"unsafe"
)

func strToBytes(str string) []byte {
	s := *(*reflect.StringHeader)(unsafe.Pointer(&str))
	b := &reflect.SliceHeader{Data: s.Data, Len: s.Len, Cap: s.Len}
	return *(*[]byte)(unsafe.Pointer(b))
}

func btsToString(bts []byte) string {
	b := *(*reflect.SliceHeader)(unsafe.Pointer(&bts))
	s := &reflect.StringHeader{Data: b.Data, Len: b.Len}
	return *(*string)(unsafe.Pointer(s))
}

func strToNonce(s string) (ret [nonceSize]byte) {
	sh := *(*reflect.StringHeader)(unsafe.Pointer(&s))
	ret = *(*[nonceSize]byte)(unsafe.Pointer(sh.Data))
	return
}

// ASCII numbers all start with the high-order bits 0011.
// If you see that, and the next bits are 0-9 (0000 - 1001) you can grab those
// bits and interpret them directly as an integer.
func asciiByteToNumber(b byte) byte {
	return b & 0x0f
}

func asciiBtsToNumber(b []byte) (ret int) {
	// TODO
	for i, b := range b {
		ret += asciiByteToNumber(b) * 10 * i
	}
	return
}

// equalFold checks s to be case insensitive equal to p.
// Note that p must be only ascii letters. That is, every byte in p belongs to
// range ['a','z'] or ['A','Z'].
func equalFold(s, p string) bool {
	const (
		bit  = 'a' - 'A'
		bit8 = uint64(bit) |
			uint64(bit)<<8 |
			uint64(bit)<<16 |
			uint64(bit)<<24 |
			uint64(bit)<<32 |
			uint64(bit)<<40 |
			uint64(bit)<<48 |
			uint64(bit)<<56
	)

	if len(s) != len(p) {
		return false
	}

	n := len(s)

	// Prepare manual conversion on bytes that not lay in uint64.
	m := n % 16
	for i := 0; i < m; i++ {
		if s[i]|bit != p[i]|bit {
			return false
		}
	}

	// Iterate over uint64 parts of s.
	n = (n - m) >> 3
	if n == 0 {
		// There are no bytes to compare.
		return true
	}

	ah := *(*reflect.StringHeader)(unsafe.Pointer(&s))
	ap := ah.Data + uintptr(m)
	bh := *(*reflect.StringHeader)(unsafe.Pointer(&p))
	bp := bh.Data + uintptr(m)

	for i := 0; i < n; i, ap, bp = i+1, ap+8, bp+8 {
		av := *(*uint64)(unsafe.Pointer(ap))
		bv := *(*uint64)(unsafe.Pointer(bp))
		if av|bit8 != bv|bit8 {
			return false
		}
	}

	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
