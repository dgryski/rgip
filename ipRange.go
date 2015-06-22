package main

import (
	"github.com/edsrzf/mmap-go"
	"io/ioutil"
	"os"
	"reflect"
	"sort"
	"sync"
	"unsafe"
)

type ipRange struct {
	rangeFrom, rangeTo uint32
	data               int
}

type ipRangeList []ipRange

type ipRanges struct {
	ranges ipRangeList
	sync.RWMutex
}

func (r ipRangeList) Len() int           { return len(r) }
func (r ipRangeList) Less(i, j int) bool { return (r)[i].rangeTo < (r)[j].rangeTo }
func (r ipRangeList) Swap(i, j int)      { (r)[i], (r)[j] = (r)[j], (r)[i] }

func (r ipRangeList) lookup(ip32 uint32) interface{} {

	idx := sort.Search(len(r), func(i int) bool { return ip32 <= r[i].rangeTo })

	if idx != -1 && r[idx].rangeFrom <= ip32 && ip32 <= r[idx].rangeTo {
		return r[idx].data
	}

	return nil
}

func (ipr *ipRanges) lookup(ip32 uint32) int {
	ipr.Lock()
	defer ipr.Unlock()
	data := ipr.ranges.lookup(ip32)

	if data == nil {
		return 0
	}

	return data.(int)
}

func reflectByteSlice(rows []ipRange) []byte {
	// Get the slice header
	header := *(*reflect.SliceHeader)(unsafe.Pointer(&rows))

	// The length and capacity of the slice are different.
	size := int(unsafe.Sizeof(ipRange{}))
	header.Len *= size
	header.Cap *= size

	// Convert slice header to a []byte
	data := *(*[]byte)(unsafe.Pointer(&header))
	return data
}

func reflectIpRangeRows(bytes []byte) []ipRange {
	// Get the slice header
	header := *(*reflect.SliceHeader)(unsafe.Pointer(&bytes))

	// The length and capacity of the slice are different.
	size := int(unsafe.Sizeof(ipRange{}))
	header.Len /= size
	header.Cap /= size

	// Convert slice header to a []byte
	data := *(*[]ipRange)(unsafe.Pointer(&header))
	return data
}

func write(filename string, ranges []ipRange) {
	representation := reflectByteSlice(ranges)
	ioutil.WriteFile(filename, representation, 0644)
}

func mmapIpRanges(filename string) ([]ipRange, error) {
	file, err := os.OpenFile(filename, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}

	mmapFile, err := mmap.Map(file, mmap.RDONLY, 0)
	if err != nil {
		return nil, err
	}

	return reflectIpRangeRows(mmapFile), nil
}
