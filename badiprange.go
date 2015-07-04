package main

import (
	"sort"
	"sync"
	"time"
)

type badIPRecord struct {
	status  string
	expires time.Time
}

type badIPRange struct {
	rangeFrom, rangeTo uint32
	data               badIPRecord
}

type badIPRangeList []badIPRange

func (r badIPRangeList) Len() int           { return len(r) }
func (r badIPRangeList) Less(i, j int) bool { return (r)[i].rangeTo < (r)[j].rangeTo }
func (r badIPRangeList) Swap(i, j int)      { (r)[i], (r)[j] = (r)[j], (r)[i] }

// lookup returns the found value, if any, followed by a bool indicating whether the value was found
func (r badIPRangeList) lookup(ip32 uint32) (badIPRecord, bool) {
	idx := sort.Search(len(r), func(i int) bool { return ip32 <= r[i].rangeTo })

	if idx != -1 && r[idx].rangeFrom <= ip32 && ip32 <= r[idx].rangeTo {
		return r[idx].data, true
	}

	return badIPRecord{}, false
}

type badIPRanges struct {
	ranges badIPRangeList
	sync.RWMutex
}

// lookup returns the found value, if any, followed by a bool indicating whether the value was found
func (ipr *badIPRanges) lookup(ip32 uint32) (badIPRecord, bool) {
	ipr.Lock()
	defer ipr.Unlock()
	return ipr.ranges.lookup(ip32)
}

type evilIPList struct {
	badIPRanges
	lastChange time.Time
}

// lookup returns the found value, if it's not expired, or an empty string if valid value was found
func (evil *evilIPList) lookup(ip32 uint32) string {
	if evil.badIPRanges.ranges == nil {
		return ""
	}

	data, ok := evil.ranges.lookup(ip32)
	if !ok {
		return ""
	}

	if time.Now().After(data.expires) {
		return ""
	}

	return data.status
}
