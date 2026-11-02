package ws

import (
	"encoding/binary"
)

// Cipher applies XOR cipher to the payload using mask.
// Offset is used to cipher chunked data (e.g. in io.Reader implementations).
//
// To convert masked data into unmasked data, or vice versa, the following
// algorithm is applied.  The same algorithm applies regardless of the
// direction of the translation, e.g., the same steps are applied to
// mask the data as to unmask the data.
func Cipher(payload []byte, mask [4]byte, offset int) {
	n := len(payload)
	if n < 16 {
		// Too short for a single block: the general path below would end up
		// doing the same byte by byte, only with more bookkeeping.
		for i := range n {
			payload[i] ^= mask[(offset+i)%4]
		}
		return
	}

	// Position in the mask after the bytes ciphered before this call.
	mpos := offset % 4
	// Head: bytes done one by one so that the rest starts at mask[0], which
	// lets the blocks below use the mask as one 32-bit word.
	ln := remain[mpos]
	// Tail: bytes done one by one so that the middle is whole 16-byte blocks.
	rn := (n - ln) % 16

	for i := range ln {
		payload[i] ^= mask[(mpos+i)%4]
	}
	for i := n - rn; i < n; i++ {
		payload[i] ^= mask[(mpos+i)%4]
	}

	// The mask as a little-endian word regardless of the machine's byte
	// order; the kernels broadcast it and xor it against little-endian loads,
	// so byte k of every word meets mask[k%4].
	m := binary.LittleEndian.Uint32(mask[:])
	cipherBlocks(payload[ln:n-rn], m)
}

// remain maps position in masking key [0,4) to number
// of bytes that need to be processed manually inside Cipher().
var remain = [4]int{0, 3, 2, 1}
