package pmath

import (
	"fmt"
	"reflect"
	"testing"
)

func TestLogarithmicRange(t *testing.T) {
	for _, test := range []struct {
		min, max int
		exp      []int
	}{
		{0, 8, []int{1, 2, 4, 8}},
		{0, 7, []int{1, 2, 4}},
		{0, 9, []int{1, 2, 4, 8}},
		{3, 8, []int{4, 8}},
		{1, 7, []int{1, 2, 4}},
		{1, 9, []int{1, 2, 4, 8}},
	} {
		t.Run("", func(t *testing.T) {
			var act []int
			LogarithmicRange(test.min, test.max, func(n int) {
				act = append(act, n)
			})
			if !reflect.DeepEqual(act, test.exp) {
				t.Errorf("unexpected range from %d to %d: %v; want %v", test.min, test.max, act, test.exp)
			}
		})
	}
}

func TestCeilToPowerOfTwo(t *testing.T) {
	for _, test := range []struct {
		in    int
		exp   int
		panic bool
	}{
		{in: 0, exp: 0},
		{in: 1, exp: 1},
		{in: 2, exp: 2},
		{in: 3, exp: 4},
		{in: 4, exp: 4},
		{in: 9, exp: 16},

		{in: maxintHeadBit - 1, exp: maxintHeadBit},
		{in: maxintHeadBit + 1, panic: true},
	} {
		t.Run(fmt.Sprintf("%d to %d", test.in, test.exp), func(t *testing.T) {
			defer func() {
				err := recover()
				if !test.panic && err != nil {
					t.Fatalf("panic: %v", err)
				}
				if test.panic && err == nil {
					t.Fatalf("want panic")
				}
			}()
			act := CeilToPowerOfTwo(test.in)
			if exp := test.exp; act != exp {
				t.Errorf("CeilToPowerOfTwo(%d) = %d; want %d", test.in, act, exp)
			}
		})
	}
}

func TestFloorToPowerOfTwo(t *testing.T) {
	for _, test := range []struct {
		in  int
		exp int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 2},
		{4, 4},
		{9, 8},
		{maxint, maxintHeadBit},
	} {
		t.Run(fmt.Sprintf("%d to %d", test.in, test.exp), func(t *testing.T) {
			act := FloorToPowerOfTwo(test.in)
			if exp := test.exp; act != exp {
				t.Errorf("FloorToPowerOfTwo(%d) = %d; want %d", test.in, act, exp)
			}
		})
	}
}

func TestIsPowerOfTwo(t *testing.T) {
	for _, test := range []struct {
		in  int
		exp bool
	}{
		{0, true},
		{1, true},
		{3, false},
		{maxint, false},
		{maxintHeadBit, true},
	} {
		t.Run(fmt.Sprintf("%d->%t", test.in, test.exp), func(t *testing.T) {
			if act, exp := IsPowerOfTwo(test.in), test.exp; act != exp {
				t.Errorf("IsPowerOfTwo(%d) = %t; want %t", test.in, act, exp)
			}
		})
	}
}

func TestFillBits(t *testing.T) {
	for _, test := range []struct {
		in  int
		exp int
	}{
		{0, 0},
		{1, 1},
		{btoi("0100"), btoi("0111")},
		{btoi("0101"), btoi("0111")},
		{maxintHeadBit, maxint},
	} {
		t.Run(fmt.Sprintf("%v", test.in), func(t *testing.T) {
			act := fillBits(test.in)
			if exp := test.exp; act != exp {
				t.Errorf(
					"fillBits(%064b) = %064b; want %064b",
					test.in, act, exp,
				)
			}
		})
	}
}

func btoi(s string) (n int) {
	fmt.Sscanf(s, "%b", &n)
	return n
}
