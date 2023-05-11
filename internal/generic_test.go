package pool

import "testing"

func TestGenericPoolGet(t *testing.T) {
	for _, test := range []struct {
		name     string
		min, max int
		get      int
		expSize  int
	}{
		{
			min:     0,
			max:     1,
			get:     10,
			expSize: 10,
		},
		{
			min:     0,
			max:     16,
			get:     10,
			expSize: 16,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := New(test.min, test.max)
			_, n := p.Get(test.get)
			if n != test.expSize {
				t.Errorf("Get(%d) = _, %d; want %d", test.get, n, test.expSize)
			}
		})
	}
}
