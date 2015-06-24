package main

import (
	"fmt"
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

func (r badIpRangeList) lookup(ip32 uint32) (BadIPRecord, error) {
	idx := sort.Search(len(r), func(i int) bool { return ip32 <= r[i].rangeTo })

	if idx != -1 && r[idx].rangeFrom <= ip32 && ip32 <= r[idx].rangeTo {
		return r[idx].data, nil
	}

	return BadIPRecord{}, fmt.Errorf("ip %d not found", ip32)
}

type badIpRanges struct {
	ranges badIpRangeList
	sync.RWMutex
}

func (ipr *badIpRanges) lookup(ip32 uint32) (BadIPRecord, error) {
	ipr.Lock()
	defer ipr.Unlock()
	return ipr.ranges.lookup(ip32)
}

type EvilIPList struct {
	badIpRanges
	lastChange time.Time
}

func (evil *EvilIPList) lookup(ip32 uint32) string {
	if evil.badIpRanges.ranges == nil {
		return ""
	}

	data, err := evil.ranges.lookup(ip32)
	if err != nil {
		return ""
	}

	if time.Now().After(data.expires) {
		return ""
	}

	return data.status
}
