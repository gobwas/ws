//go:build go1.8
// +build go1.8

package main

import "sort"

func sortBySegment(s []string) {
	sort.Slice(s, func(i, j int) bool {
		return compareBySegment(s[i], s[j]) < 0
	})
}
