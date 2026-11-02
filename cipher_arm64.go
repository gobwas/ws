//go:build !purego

package ws

// cipherBlocks xors p with key repeated, using NEON. len(p) must be a multiple
// of 16 and key must be the mask as a little-endian uint32, aligned to p[0].
//
//go:noescape
func cipherBlocks(p []byte, key uint32)
