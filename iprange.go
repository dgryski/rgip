package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/dgryski/rgip/mlog"
)

var magicBytes = []byte{'r', 'g', 'i', 'p', 'M', 'a', 'p', 0}

const ipRangeSize = 12

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

	if idx < len(r) && r[idx].rangeFrom <= ip32 && ip32 <= r[idx].rangeTo {
		return r[idx].data, true
	}

	return 0, false
}

// lookup returns the found value, if any, followed by a bool indicating whether the value was found
func (ipr *ipRanges) lookup(ip32 uint32) (int32, bool) {
	ipr.RLock()
	defer ipr.RUnlock()
	return ipr.ranges.lookup(ip32)
}

func readMagicBytes(file io.Reader, name string) error {
	b := make([]byte, len(magicBytes))
	_, err := io.ReadFull(file, b)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", name, err)
	}

	if !bytes.Equal(b, magicBytes) {
		return fmt.Errorf("file format is incorrect, expected %s '%s', actual '%s'", name, magicBytes, b)
	}

	return nil
}

func loadIPRangesFromBinary(file io.Reader) ([]ipRange, error) {
	err := readMagicBytes(file, "header")
	if err != nil {
		return nil, err
	}

	lenranges := make([]byte, 4)
	_, err = io.ReadFull(file, lenranges)
	if err != nil {
		return nil, fmt.Errorf("can't read file size field %s", err)
	}

	ranges := make([]ipRange, binary.LittleEndian.Uint32(lenranges))
	b := make([]byte, ipRangeSize)
	for i := range ranges {
		_, err = io.ReadFull(file, b)
		if err != nil {
			return nil, fmt.Errorf("expected %d items, got %d", len(ranges), i)
		}

		ranges[i] = ipRange{
			binary.LittleEndian.Uint32(b[0:]),
			binary.LittleEndian.Uint32(b[4:]),
			int32(binary.LittleEndian.Uint32(b[8:])),
		}
	}

	err = readMagicBytes(file, "footer")
	if err != nil {
		return nil, err
	}

	return ranges, nil
}

func writeBinary(file *os.File, ranges []ipRange) error {

	f := bufio.NewWriter(file)

	if _, err := f.Write(magicBytes); err != nil {
		return err
	}

	var b [ipRangeSize]byte

	binary.LittleEndian.PutUint32(b[0:], uint32(len(ranges)))
	if _, err := f.Write(b[:4]); err != nil {
		return err
	}

	for _, r := range ranges {
		binary.LittleEndian.PutUint32(b[0:], r.rangeFrom)
		binary.LittleEndian.PutUint32(b[4:], r.rangeTo)
		binary.LittleEndian.PutUint32(b[8:], uint32(r.data))
		if _, err := f.Write(b[:]); err != nil {
			return err
		}
	}

	if _, err := f.Write(magicBytes); err != nil {
		return err
	}

	if err := f.Flush(); err != nil {
		return err
	}
	return nil
}

func loadIPRangesFromCSV(file *os.File) (ipRangeList, error) {
	svr := csv.NewReader(file)

	var ips ipRangeList

	prevIP := -1

	for {
		r, err := svr.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			mlog.Println("error reading CSV: ", err)
			return nil, err
		}

		var ipFrom, ipTo, data int

		var convert converr
		ipFrom = prevIP + 1
		ipTo = convert.check(r[0])
		data = convert.check(r[1])
		prevIP = ipTo

		if convert.err != nil {
			mlog.Printf("error parsing %v: %s", r, err)
			return nil, convert.err
		}

		ips = append(ips, ipRange{rangeFrom: uint32(ipFrom), rangeTo: uint32(ipTo), data: int32(data)})
	}

	return ips, nil
}

type converr struct {
	err error
}

func (c *converr) check(s string) int {
	i, e := strconv.Atoi(s)
	if e != nil {
		c.err = e
		return 0
	}
	return i
}

func loadIPRanges(fname string, isbinary bool) (ipRangeList, error) {
	file, err := os.Open(fname)
	if err != nil {
		mlog.Println("can't open file: ", fname, err)
		return nil, err
	}

	defer file.Close()
	if isbinary {
		return loadIPRangesFromBinary(bufio.NewReader(file))
	}

	return loadIPRangesFromCSV(file)
}
