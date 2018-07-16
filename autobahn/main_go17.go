// +build !go1.8

package main

import "sort"

func sortBySegment(s []string) {
	sort.Sort(segmentSorter(s))
}

type segmentSorter []string

func (s segmentSorter) Less(i, j int) bool {
	return compareBySegment(s[i], s[j]) < 0
}

func (s segmentSorter) Len() int {
	return len(s)
}

func (s segmentSorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
