package ws

import (
	"reflect"
	"unsafe"
)

// Cipher applies XOR cipher to the payload using mask.
// Offset is used to cipher chunked data (e.g. in io.Reader implementations).
//
// To convert masked data into unmasked data, or vice versa, the following
// algorithm is applied.  The same algorithm applies regardless of the
// direction of the translation, e.g., the same steps are applied to
// mask the data as to unmask the data.
func Cipher(payload, mask []byte, offset int) {
	if len(mask) != 4 {
		return
	}

	n := len(payload)
	if n < 8 {
		for i := 0; i < n; i++ {
			payload[i] ^= mask[(offset+i)%4]
		}
		return
	}

	// Calculate position in mask due to previously processed bytes number.
	mpos := offset % 4
	// Count number of bytes will processed one by one from the begining of payload.
	// Bitwise used to avoid additional if.
	ln := (4 - mpos) & 0x0b
	// Count number of bytes will processed one by one from the end of payload.
	// This is done to process payload by 8 bytes in each iteration of main loop.
	rn := (n - ln) % 8

	for i := 0; i < ln; i++ {
		payload[i] ^= mask[(mpos+i)%4]
	}
	for i := n - rn; i < n; i++ {
		payload[i] ^= mask[(mpos+i)%4]
	}

	ph := *(*reflect.SliceHeader)(unsafe.Pointer(&payload))
	mh := *(*reflect.SliceHeader)(unsafe.Pointer(&mask))

	m := *(*uint32)(unsafe.Pointer(mh.Data))
	m2 := uint64(m)<<32 | uint64(m)

	// Process the rest of bytes as uint64.
	for i := ln; i+8 <= n-rn; i += 8 {
		v := (*uint64)(unsafe.Pointer(ph.Data + uintptr(i)))
		*v = *v ^ m2
	}
}
