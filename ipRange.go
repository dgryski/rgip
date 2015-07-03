package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"github.com/edsrzf/mmap-go"
	"io"
	"log"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"unsafe"
)

var magicBytes = []byte{'r', 'g', 'i', 'p', 'M', 'a', 'p', 0}

const ipRangeSize int = int(unsafe.Sizeof(ipRange{}))

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

	header.Len *= ipRangeSize
	header.Cap *= ipRangeSize

	data := *(*[]byte)(unsafe.Pointer(&header))
	return data
}

func reflectIpRangeRows(data []byte) ([]ipRange, error) {
	dataLength := len(data) - 2*len(magicBytes)
	if dataLength < 0 {
		return nil, fmt.Errorf("file is too small for the expected format")
	}

	head := data[:len(magicBytes)]
	if !bytes.Equal(head, magicBytes) {
		return nil, fmt.Errorf("file format is incorrect, expected header '%s', actual '%s'", magicBytes, head)
	}

	footer := data[len(data)-len(magicBytes):]
	if !bytes.Equal(footer, magicBytes) {
		return nil, fmt.Errorf("file format is incorrect, expected footer '%s', actual '%s'", magicBytes, footer)
	}

	data = data[len(magicBytes) : len(data)-len(magicBytes)]
	header := *(*reflect.SliceHeader)(unsafe.Pointer(&data))
	header.Len /= ipRangeSize
	header.Cap /= ipRangeSize
	return *(*[]ipRange)(unsafe.Pointer(&header)), nil
}

func writeMmap(file *os.File, ranges []ipRange) error {
	_, err := file.Write(magicBytes)
	if err != nil {
		return err
	}

	_, err = file.Write(reflectByteSlice(ranges))
	if err != nil {
		return err
	}

	_, err = file.Write(magicBytes)
	return err
}

func mmapIpRanges(file *os.File) ([]ipRange, error) {
	mmapFile, err := mmap.Map(file, mmap.RDONLY, 0)
	if err != nil {
		return nil, err
	}

	return reflectIpRangeRows(mmapFile)
}

func loadIpRangesFromCSV(fname string) (ipRangeList, error) {
	file, err := os.Open(fname)
	if err != nil {
		log.Println("can't open file: ", fname, err)
		return nil, err
	}

	defer file.Close()
	svr := csv.NewReader(file)

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
		file, err := os.OpenFile(fname, os.O_RDONLY, 0644)
		if err != nil {
			log.Println("can't open file: ", fname, err)
			return nil, err
		}

		defer file.Close()
		return mmapIpRanges(file)
	}

	return loadIpRangesFromCSV(fname)
}
