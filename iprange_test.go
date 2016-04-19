package main

import (
	"io/ioutil"
	"math/rand"
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
	actual, err := loadIPRangesFromBinary(tempFile)
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

func BenchmarkFileLoad(b *testing.B) {
	for n := 0; n < b.N; n++ {
		fname := "maxmind/GeoIPRange_dump.csv.bin"
		ranges, err := loadIPRanges(fname, true)
		if err != nil {
			b.Errorf("couldn't load %s: %s", fname, err)
		}

		if len(ranges) < 1000 {
			b.Errorf("loaded only %d entries", len(ranges))
		}
	}
}

var total int32

func BenchmarkLookup(b *testing.B) {
	fname := "maxmind/GeoIPRange_dump.csv.bin"
	ranges, err := loadIPRanges(fname, true)
	if err != nil {
		b.Errorf("couldn't load %s: %s", fname, err)
	}

	rand.Seed(0)
	b.ResetTimer()
	total = 0
	for i := 0; i < b.N; i++ {
		d, _ := ranges.lookup(randomIP())
		total += d
	}
}

const benchLimit = 1e7

func TestNormalLookup(t *testing.T) {
	fname := "maxmind/GeoIPRange_dump.csv.bin"
	ranges, err := loadIPRanges(fname, true)
	if err != nil {
		t.Errorf("couldn't load %s: %s", fname, err)
	}

	t.Log("normal size=", len(ranges))

	rand.Seed(0)
	total = 0
	for i := 0; i < benchLimit; i++ {
		d, _ := ranges.lookup(uint32(rand.Int63()))
		total += d
	}

	t.Log("total", total)
}

func TestShardedLookup(t *testing.T) {
	fname := "maxmind/GeoIPRange_dump.csv.bin"
	ranges, err := loadIPRanges(fname, true)
	if err != nil {
		t.Errorf("couldn't load %s: %s", fname, err)
	}

	shards, err := ranges.shard()
	if err != nil {
		panic(err)
	}

	total = 0
	for i, s := range shards {
		total += int32(len(s))
		t.Logf("shard[%x]=%d", i, len(s))
	}
	t.Log("sharded size=", total)

	rand.Seed(0)
	total = 0
	for i := 0; i < benchLimit; i++ {
		d, _ := shards.lookup(randomIP())
		total += d
	}

	t.Log("total", total)
}

func randomIP() uint32 {
	ip := uint32(rand.Int63())
	if ip&0xff000000 > 0xdf000000 {
		ip -= 0xdf000000
	}
	return ip
}

func BenchmarkShardedLookup(b *testing.B) {
	fname := "maxmind/GeoIPRange_dump.csv.bin"
	ranges, err := loadIPRanges(fname, true)
	if err != nil {
		b.Errorf("couldn't load %s: %s", fname, err)
	}

	shards, err := ranges.shard()
	if err != nil {
		panic(err)
	}

	rand.Seed(0)
	b.ResetTimer()
	total = 0
	for i := 0; i < b.N; i++ {
		d, _ := shards.lookup(randomIP())
		total += d
	}
}
