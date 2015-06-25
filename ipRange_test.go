package main

import (
	"io/ioutil"
	"os"
	"testing"
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

	tempFile, err := ioutil.TempFile("", "rgipMmap")
	if err != nil {
		t.Errorf("couldn't create temp file")
	}

	t.Logf("filename %s", tempFile.Name())
	defer os.Remove(tempFile.Name())
	writeMmap(tempFile, want)
	actual, err := mmapIpRanges(tempFile)
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
