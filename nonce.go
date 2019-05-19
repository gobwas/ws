package ws

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"reflect"
	"sync"
	"unsafe"
)

const (
	// RFC6455: The value of this header field MUST be a nonce consisting of a
	// randomly selected 16-byte value that has been base64-encoded (see
	// Section 4 of [RFC4648]).  The nonce MUST be selected randomly for each
	// connection.
	nonceKeySize = 16
	nonceSize    = 24 // base64.StdEncoding.EncodedLen(nonceKeySize)

	// RFC6455: The value of this header field is constructed by concatenating
	// /key/, defined above in step 4 in Section 4.2.2, with the string
	// "258EAFA5- E914-47DA-95CA-C5AB0DC85B11", taking the SHA-1 hash of this
	// concatenated value to obtain a 20-byte value and base64- encoding (see
	// Section 4 of [RFC4648]) this 20-byte hash.
	acceptSize = 28 // base64.StdEncoding.EncodedLen(sha1.Size)
)

var webSocketMagic = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

var sha1Pool sync.Pool

// nonce helps to put nonce bytes on the stack and then retrieve stack-backed
// slice with unsafe.
type nonce [nonceSize]byte

// bytes returns slice of bytes backed by nonce array.
// Note that returned slice is only valid until nonce array is alive.
func (n *nonce) bytes() (bts []byte) {
	h := (*reflect.SliceHeader)(unsafe.Pointer(&bts))
	*h = reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(n)),
		Len:  len(n),
		Cap:  len(n),
	}
	return bts
}

func acquireSha1() hash.Hash {
	if h := sha1Pool.Get(); h != nil {
		return h.(hash.Hash)
	}
	return sha1.New()
}

func releaseSha1(h hash.Hash) {
	h.Reset()
	sha1Pool.Put(h)
}

// initNonce fills given slice with random base64-encoded nonce bytes.
func initNonce(dst []byte) {
	// NOTE: bts does not escapes.
	bts := make([]byte, nonceKeySize)
	if _, err := rand.Read(bts); err != nil {
		panic(fmt.Sprintf("rand read error: %s", err))
	}
	base64.StdEncoding.Encode(dst, bts)
}

// checkAcceptFromNonce reports whether given accept bytes are valid for given
// nonce bytes.
func checkAcceptFromNonce(accept, nonce []byte) bool {
	if len(accept) != acceptSize {
		return false
	}
	// NOTE: expect does not escapes.
	expect := make([]byte, acceptSize)
	initAcceptFromNonce(expect, nonce)
	return bytes.Equal(expect, accept)
}

// initAcceptFromNonce fills given slice with accept bytes generated from given
// nonce bytes. Given buffer should be exactly acceptSize bytes.
func initAcceptFromNonce(dst, nonce []byte) {
	if len(dst) != acceptSize {
		panic("accept buffer is invalid")
	}
	if len(nonce) != nonceSize {
		panic("nonce is invalid")
	}

	sha := acquireSha1()
	defer releaseSha1(sha)

	sha.Write(nonce)
	sha.Write(webSocketMagic)

	var (
		sb  [sha1.Size]byte
		sum []byte
	)
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&sum))
	*sh = reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(&sb)),
		Len:  0,
		Cap:  len(sb),
	}
	sum = sha.Sum(sum)

	base64.StdEncoding.Encode(dst, sum)
}

func writeAccept(w io.Writer, nonce []byte) (int, error) {
	var (
		b   [acceptSize]byte
		bts []byte
	)
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&bts))
	*bh = reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(&b)),
		Len:  len(b),
		Cap:  len(b),
	}

	initAcceptFromNonce(bts, nonce)

	return w.Write(bts)
}
