//go:build !purego

package ws

import "unsafe"

func strToBytes(str string) []byte {
	return unsafe.Slice(unsafe.StringData(str), len(str))
}

func btsToString(bts []byte) string {
	return unsafe.String(unsafe.SliceData(bts), len(bts))
}
