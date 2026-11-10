package ws

import "testing"

func TestCheckCloseFrameData(t *testing.T) {
	for _, test := range []struct {
		code StatusCode
		err  error
	}{
		{
			code: StatusNormalClosure,
			err:  nil,
		},
		{
			code: StatusInternalServerError,
			err:  nil,
		},
		{
			code: StatusServiceRestart,
			err:  nil,
		},
		{
			code: StatusTryAgainLater,
			err:  nil,
		},
		{
			code: StatusBadGateway,
			err:  nil,
		},
		{
			code: 999,
			err:  ErrProtocolStatusCodeNotInUse,
		},
		{
			code: StatusNoMeaningYet,
			err:  ErrProtocolStatusCodeNoMeaning,
		},
		{
			code: StatusNoStatusRcvd,
			err:  ErrProtocolStatusCodeApplicationLevel,
		},
		{
			code: StatusTLSHandshake,
			err:  ErrProtocolStatusCodeApplicationLevel,
		},
		{
			code: 1016,
			err:  ErrProtocolStatusCodeUnknown,
		},
		{
			code: 2999,
			err:  ErrProtocolStatusCodeUnknown,
		},
		{
			code: 3000,
			err:  nil,
		},
		{
			code: 4999,
			err:  nil,
		},
	} {
		if err := CheckCloseFrameData(test.code, ""); err != test.err {
			t.Errorf(
				"CheckCloseFrameData(%d) = %v; want %v",
				test.code, err, test.err,
			)
		}
	}
}
