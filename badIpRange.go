package main

import (
	"sort"
	"sync"
	"time"
)

type BadIPRecord struct {
	status  string
	expires time.Time
}

type badIpRange struct {
	rangeFrom, rangeTo uint32
	data               BadIPRecord
}

type badIpRangeList []badIpRange

func (r badIpRangeList) Len() int           { return len(r) }
func (r badIpRangeList) Less(i, j int) bool { return (r)[i].rangeTo < (r)[j].rangeTo }
func (r badIpRangeList) Swap(i, j int)      { (r)[i], (r)[j] = (r)[j], (r)[i] }

func (r badIpRangeList) lookup(ip32 uint32) interface{} {

	idx := sort.Search(len(r), func(i int) bool { return ip32 <= r[i].rangeTo })

	if idx != -1 && r[idx].rangeFrom <= ip32 && ip32 <= r[idx].rangeTo {
		return r[idx].data
	}

	return nil
}

type badIpRanges struct {
	ranges badIpRangeList
	sync.RWMutex
}

func (ipr *badIpRanges) lookup(ip32 uint32) interface{} {
	ipr.Lock()
	defer ipr.Unlock()
	data := ipr.ranges.lookup(ip32)

	if data == nil {
		return 0
	}

	return data
}

type EvilIPList struct {
	badIpRanges
	lastChange time.Time
}

func (evil *EvilIPList) lookup(ip32 uint32) string {
	if evil.badIpRanges.ranges == nil {
		return ""
	}

	data := evil.ranges.lookup(ip32)
	if data == nil {
		return ""
	}

	val := data.(BadIPRecord)

	if time.Now().After(val.expires) {
		return ""
	}

	return val.status
}
