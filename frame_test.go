package ws

import (
	"fmt"
	"testing"
)

func TestOpCodeIsControl(t *testing.T) {
	for _, test := range []struct {
		code OpCode
		exp  bool
	}{
		{OpClose, true},
		{OpPing, true},
		{OpPong, true},
		{OpBinary, false},
		{OpText, false},
		{OpContinuation, false},
	} {
		t.Run(fmt.Sprintf("0x%02x", test.code), func(t *testing.T) {
			if act := test.code.IsControl(); act != test.exp {
				t.Errorf("IsControl = %v; want %v", act, test.exp)
			}
		})
	}
}
