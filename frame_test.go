package ws

import (
	"bytes"
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

func TestCompiledClose(t *testing.T) {
	for _, test := range []struct {
		frame []byte
		code  StatusCode
		value StatusCode
	}{
		{CompiledCloseServiceRestart, StatusServiceRestart, 1012},
		{CompiledCloseTryAgainLater, StatusTryAgainLater, 1013},
		{CompiledCloseBadGateway, StatusBadGateway, 1014},
	} {
		t.Run(fmt.Sprintf("%d", test.value), func(t *testing.T) {
			if test.code != test.value {
				t.Fatalf("code = %d; want %d", test.code, test.value)
			}
			f, err := ReadFrame(bytes.NewReader(test.frame))
			if err != nil {
				t.Fatal(err)
			}
			if f.Header.OpCode != OpClose || !f.Header.Fin {
				t.Fatalf("unexpected header: %+v", f.Header)
			}
			if code, reason := ParseCloseFrameData(f.Payload); code != test.code || reason != "" {
				t.Errorf(
					"ParseCloseFrameData() = %d, %q; want %d, %q",
					code, reason, test.code, "",
				)
			}
		})
	}
}
