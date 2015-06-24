package main

import (
	"testing"
	"time"
)

func TestWriteMmapAndReadAgain(t *testing.T) {
	want := []ipRange{
		{387534209, 387534209, 20107093},
		{387534210, 387534210, 20107094},
		{387534211, 387534211, 20107095},
		{387534212, 387534212, 20107096},
		{387534213, 387534213, 20107097},
		{387534214, 387534214, 20107098},
	}

	filename := "maxmind/mmap.test"
	start := time.Now()
	writeMmap(filename, want)
	t.Logf("took %v seconds to write the mmap", time.Since(start).Seconds())
	start = time.Now()
	actual, err := mmapIpRanges(filename)
	if err != nil {
		t.Errorf("couldn't load %s", filename)
	}

	t.Logf("took %v seconds to read the mmap", time.Since(start).Seconds())

	if len(want) != len(actual) {
		t.Errorf("length of input and output arrays is different, want %d, got %d", len(want), len(actual))
	}

	for i := range want {
		if want[i].rangeFrom != actual[i].rangeFrom {
			t.Errorf("want %d, got %d", want[i].rangeFrom, actual[i].rangeFrom)
		}
	}
}
