package main

import (
	"encoding/csv"
	"fmt"
	"github.com/edsrzf/mmap-go"
	"io"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"unsafe"
)

type ipRange struct {
	rangeFrom, rangeTo uint32
	data               int32
}

type ipRangeList []ipRange

type ipRanges struct {
	ranges ipRangeList
	sync.RWMutex
}

func (r ipRangeList) Len() int           { return len(r) }
func (r ipRangeList) Less(i, j int) bool { return (r)[i].rangeTo < (r)[j].rangeTo }
func (r ipRangeList) Swap(i, j int)      { (r)[i], (r)[j] = (r)[j], (r)[i] }

// lookup returns the found value, if any, followed by a bool indicating whether the value was found
func (r ipRangeList) lookup(ip32 uint32) (int32, bool) {
	idx := sort.Search(len(r), func(i int) bool { return ip32 <= r[i].rangeTo })

	if idx != -1 && r[idx].rangeFrom <= ip32 && ip32 <= r[idx].rangeTo {
		return r[idx].data, true
	}

	return 0, false
}

// lookup returns the found value, if any, followed by a bool indicating whether the value was found
func (ipr *ipRanges) lookup(ip32 uint32) (int32, bool) {
	ipr.Lock()
	defer ipr.Unlock()
	return ipr.ranges.lookup(ip32)
}

func reflectByteSlice(rows []ipRange) []byte {
	header := *(*reflect.SliceHeader)(unsafe.Pointer(&rows))

	size := int(unsafe.Sizeof(ipRange{}))
	header.Len *= size
	header.Cap *= size

	data := *(*[]byte)(unsafe.Pointer(&header))
	return data
}

func reflectIpRangeRows(bytes []byte) ([]ipRange, error) {
	header := *(*reflect.SliceHeader)(unsafe.Pointer(&bytes))

	size := int(unsafe.Sizeof(ipRange{}))
	if header.Len%size != 0 {
		return nil, fmt.Errorf("the length of the byte array %d isn't a multiple of the size of an ipRange %d", header.Len, size)
	}

	header.Len /= size
	header.Cap /= size

	data := *(*[]ipRange)(unsafe.Pointer(&header))
	return data, nil
}

func writeMmap(filename string, ranges []ipRange) {
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

	return reflectIpRangeRows(mmapFile)
}

func loadIpRangesFromCSV(fname string) (ipRangeList, error) {
	f, err := os.Open(fname)
	if err != nil {
		log.Println("can't open file: ", fname, err)
		return nil, err
	}
	defer f.Close()

	svr := csv.NewReader(f)

	var ips ipRangeList

	prevIP := -1

	for {
		r, err := svr.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Println("error reading CSV: ", err)
			return nil, err
		}

		var ipFrom, ipTo, data int

		var convert converr
		ipFrom = prevIP + 1
		ipTo = convert.check(r[0], strconv.Atoi)
		data = convert.check(r[1], strconv.Atoi)
		prevIP = ipTo

		if convert.err != nil {
			log.Printf("error parsing %v: %s", r, err)
			return nil, convert.err
		}

		ips = append(ips, ipRange{rangeFrom: uint32(ipFrom), rangeTo: uint32(ipTo), data: int32(data)})
	}

	return ips, nil
}

func loadIpRanges(fname string, usemmap bool) (ipRangeList, error) {
	if usemmap {
		return mmapIpRanges(fname)
	}

	return loadIpRangesFromCSV(fname)
}
