package main

import (
	"testing"
)

func TestReadWrite(t *testing.T) {
	filename := "maxmind/testfile.mmap"
	want := []ipRange{
		{387534209, 387534209, uint32(20107093)},
		{387534210, 387534210, uint32(20107094)},
		{387534211, 387534211, uint32(20107095)},
		{387534212, 387534212, uint32(20107096)},
		{387534213, 387534213, uint32(20107097)},
		{387534214, 387534214, uint32(20107098)},
	}
	write(filename, want)
	actual, err := mmapIpRanges(filename)
	if err != nil {
		t.Errorf("error reading", filename, err)
	}

	if len(want) != len(actual) {
		t.Errorf("length of input and output arrays is different, want %d, got %d", len(want), len(actual))
	}

	for i := range want {
		if want[i].rangeFrom != actual[i].rangeFrom {
			t.Errorf("want %d, got %d", want[i].rangeFrom, actual[i].rangeFrom)
		}
	}
}
