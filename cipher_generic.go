//go:build purego || !(amd64 || arm64)

package ws

import "encoding/binary"

// cipherBlocks xors p with key repeated. len(p) must be a multiple of 16 and
// key must be the mask as a little-endian uint32, aligned to p[0].
//
// This is the portable version; the assembly ones live next to it.
func cipherBlocks(p []byte, key uint32) {
	m2 := uint64(key)<<32 | uint64(key)
	for ; len(p) >= 16; p = p[16:] {
		a := binary.LittleEndian.Uint64(p) ^ m2
		b := binary.LittleEndian.Uint64(p[8:]) ^ m2
		binary.LittleEndian.PutUint64(p, a)
		binary.LittleEndian.PutUint64(p[8:], b)
	}
}
