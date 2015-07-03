package main

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestWriteBinaryAndReadAgain(t *testing.T) {
	want := []ipRange{
		{387534209, 387534209, 20107093},
		{387534210, 387534210, 20107094},
		{387534211, 387534211, 20107095},
		{387534212, 387534212, 20107096},
		{387534213, 387534213, 20107097},
		{387534214, 387534214, 20107098},
	}

	tempFile, err := ioutil.TempFile("", "rgipBinary")
	if err != nil {
		t.Errorf("couldn't create temp file")
	}

	t.Logf("filename %s", tempFile.Name())
	defer os.Remove(tempFile.Name())
	writeBinary(tempFile, want)
	tempFile.Seek(0, 0)
	actual, err := loadIpRangesFromBinary(tempFile)
	if err != nil {
		t.Errorf("couldn't load %s: %s", tempFile.Name(), err)
		return
	}

	if len(want) != len(actual) {
		t.Errorf("length of input and output arrays is different, want %d, got %d", len(want), len(actual))
		return
	}

	for i := range want {
		t.Logf("i %d", i)
		if want[i].rangeFrom != actual[i].rangeFrom {
			t.Errorf("want %d, got %d", want[i].rangeFrom, actual[i].rangeFrom)
		}
	}
}

func Benchmark(b *testing.B) {
	for n := 0; n < b.N; n++ {
		fname := "maxmind/GeoIPRange_dump.csv.bin"
		ranges, err := loadIpRanges(fname, true)
		if err != nil {
			b.Errorf("couldn't load %s: %s", fname, err)
		}

		if len(ranges) < 1000 {
			b.Errorf("loaded only %d entries", len(ranges))
		}
	}
}
