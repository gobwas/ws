package ws

import (
	"reflect"
	"unsafe"
)

var remain = [4]int{0, 3, 2, 1}

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
	ln := remain[mpos]
	// Count number of bytes will processed one by one from the end of payload.
	// This is done to process payload by 8 bytes in each iteration of main loop.
	rn := (n - ln) % 8

	for i := 0; i < ln; i++ {
		payload[i] ^= mask[(mpos+i)%4]
	}
	for i := n - rn; i < n; i++ {
		payload[i] ^= mask[(mpos+i)%4]
	}

	mh := *(*reflect.SliceHeader)(unsafe.Pointer(&mask))
	m := *(*uint32)(unsafe.Pointer(mh.Data))
	m2 := uint64(m)<<32 | uint64(m)

	// Get pointer to payload at ln index to
	// skip manual processed bytes above.
	p := uintptr(unsafe.Pointer(&payload[ln]))
	// Also skip right part as the division by 8 remainder.
	// Divide it by 8 to get number of uint64 parts remaining to process.
	n = (n - rn) >> 3
	// Process the rest of bytes as uint64.
	for i := 0; i < n; i, p = i+1, p+8 {
		v := (*uint64)(unsafe.Pointer(p))
		*v = *v ^ m2
	}
}
