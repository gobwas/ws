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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
