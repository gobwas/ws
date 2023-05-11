package pbufio

import "testing"

func TestGetWriter(t *testing.T) {
	for _, test := range []struct {
		min int
		max int
		get int
		exp int
	}{
		{
			min: 0,
			max: 100,
			get: 500,
			exp: 500,
		},
		{
			min: 0,
			max: 128,
			get: 60,
			exp: 64,
		},
	} {
		t.Run("", func(t *testing.T) {
			p := NewWriterPool(test.min, test.max)
			bw := p.Get(nil, test.get)
			if n, exp := bw.Available(), test.exp; n != exp {
				t.Errorf("unexpected Get() buffer size: %v; want %v", n, exp)
			}
		})
	}
}

func TestGetReader(t *testing.T) {
	for _, test := range []struct {
		min int
		max int
		get int
		exp int
	}{
		{
			min: 0,
			max: 100,
			get: 500,
			exp: 500,
		},
		{
			min: 0,
			max: 128,
			get: 60,
			exp: 64,
		},
	} {
		t.Run("", func(t *testing.T) {
			p := NewReaderPool(test.min, test.max)
			br := p.Get(nil, test.get)
			if n, exp := br.Size(), test.exp; n != exp {
				t.Errorf("unexpected Get() buffer size: %v; want %v", n, exp)
			}
		})
	}
}
