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

var ErrBadNonce = fmt.Errorf("nonce size is not %d", nonceSize)

var WebSocketMagic = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

var sha1Pool sync.Pool

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

func checkNonceAccept(nonce, accept []byte) bool {
	if len(accept) != acceptSize {
		return false
	}
	// NOTE: expect does not escapes.
	expect := make([]byte, acceptSize)
	putAcceptNonce(expect, nonce)
	return bytes.Equal(expect, accept)
}

//const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
//
//func newNonce(dest []byte) {
//	for i := 0; i < 22; i++ {
//		dest[i] = alphabet[rand.Intn(len(alphabet))]
//	}
//	dest[22] = '='
//	dest[23] = '='
//}

func putNewNonce(dest []byte) {
	// NOTE: bts does not escapes.
	bts := make([]byte, nonceKeySize)
	if _, err := rand.Read(bts); err != nil {
		panic(fmt.Sprintf("rand read error: %s", err))
	}
	base64.StdEncoding.Encode(dest, bts)
}

// putAcceptNonce generates accept bytes and puts them into p.
// Given buffer should be exactly acceptSize bytes.
func putAcceptNonce(dest, nonce []byte) {
	if len(dest) != acceptSize {
		panic("accept buffer is invalid")
	}
	if len(nonce) != nonceSize {
		panic("nonce is invalid")
	}

	sha := acquireSha1()
	defer releaseSha1(sha)

	sha.Write(nonce)
	sha.Write(WebSocketMagic)

	var sb [sha1.Size]byte
	sh := uintptr(unsafe.Pointer(&sb))
	sum := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: sh,
		Len:  0,
		Cap:  sha1.Size,
	}))
	sum = sha.Sum(sum)

	base64.StdEncoding.Encode(dest, sum)
}

func writeAccept(w io.Writer, nonce []byte) (int, error) {
	var b [acceptSize]byte
	bp := uintptr(unsafe.Pointer(&b))
	bts := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: bp,
		Len:  acceptSize,
		Cap:  acceptSize,
	}))

	putAcceptNonce(bts, nonce)

	return w.Write(bts)
}
