package main

import (
	"io/ioutil"
	"os"
	"reflect"
	"unsafe"

	"github.com/edsrzf/mmap-go"
)

type ipRange struct {
	rangeFrom, rangeTo uint32
	data               interface{}
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
