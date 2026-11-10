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
		{
			frame: CompiledClose,
			code:  0,
			value: 0,
		},
		{
			frame: CompiledCloseNormalClosure,
			code:  StatusNormalClosure,
			value: 1000,
		},
		{
			frame: CompiledCloseGoingAway,
			code:  StatusGoingAway,
			value: 1001,
		},
		{
			frame: CompiledCloseProtocolError,
			code:  StatusProtocolError,
			value: 1002,
		},
		{
			frame: CompiledCloseUnsupportedData,
			code:  StatusUnsupportedData,
			value: 1003,
		},
		{
			frame: CompiledCloseNoMeaningYet,
			code:  StatusNoMeaningYet,
			value: 1004,
		},
		{
			frame: CompiledCloseInvalidFramePayloadData,
			code:  StatusInvalidFramePayloadData,
			value: 1007,
		},
		{
			frame: CompiledClosePolicyViolation,
			code:  StatusPolicyViolation,
			value: 1008,
		},
		{
			frame: CompiledCloseMessageTooBig,
			code:  StatusMessageTooBig,
			value: 1009,
		},
		{
			frame: CompiledCloseMandatoryExt,
			code:  StatusMandatoryExt,
			value: 1010,
		},
		{
			frame: CompiledCloseInternalServerError,
			code:  StatusInternalServerError,
			value: 1011,
		},
		{
			frame: CompiledCloseServiceRestart,
			code:  StatusServiceRestart,
			value: 1012,
		},
		{
			frame: CompiledCloseTryAgainLater,
			code:  StatusTryAgainLater,
			value: 1013,
		},
		{
			frame: CompiledCloseBadGateway,
			code:  StatusBadGateway,
			value: 1014,
		},
		{
			frame: CompiledCloseTLSHandshake,
			code:  StatusTLSHandshake,
			value: 1015,
		},
	} {
		t.Run(fmt.Sprintf("%d", test.value), func(t *testing.T) {
			if test.code != test.value {
				t.Fatalf(
					"code = %d; want %d",
					test.code, test.value,
				)
			}
			f, err := ReadFrame(bytes.NewReader(test.frame))
			if err != nil {
				t.Fatal(err)
			}
			if f.Header.OpCode != OpClose || !f.Header.Fin {
				t.Fatalf(
					"unexpected header: %+v",
					f.Header,
				)
			}
			wantLength := 2
			if test.value == 0 {
				wantLength = 0
			}
			if len(f.Payload) != wantLength {
				t.Fatalf(
					"payload length = %d; want %d",
					len(f.Payload), wantLength,
				)
			}
			code, reason := ParseCloseFrameData(f.Payload)
			if code != test.code || reason != "" {
				t.Errorf(
					"ParseCloseFrameData() = %d, %q; want %d, %q",
					code, reason, test.code, "",
				)
			}
		})
	}
}
