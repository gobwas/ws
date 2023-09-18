//go:build !purego && go1.20
// +build !purego,go1.20

package ws

import "unsafe"

func strToBytes(str string) (bts []byte) {
	return unsafe.Slice(unsafe.StringData(str), len(str))
}

func btsToString(bts []byte) (str string) {
	return unsafe.String(&bts[0], len(bts))
}
