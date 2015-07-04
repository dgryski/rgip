package main

import (
	"testing"
	"time"
)

func TestLookupBadIp(t *testing.T) {
	unixLimit := time.Date(2038, time.January, 19, 3, 14, 8, 0, time.UTC)
	var badIps badIPRanges
	badIps.ranges = []badIPRange{
		{387534209, 387534209, BadIPRecord{"expired", time.Now()}},
		{387534210, 387534213, BadIPRecord{"bad", unixLimit}},
		{387534214, 387534214, BadIPRecord{"badder", unixLimit}},
	}
	EvilIPs := EvilIPList{
		badIps,
		time.Now(),
	}

	tests := []struct {
		name string
		ip   uint32
		want string
	}{
		{
			"out of range",
			387534201,
			"",
		},
		{
			"expired",
			387534209,
			"",
		},
		{
			"exact hit",
			387534214,
			"badder",
		},
		{
			"hit in the middle of a range",
			387534212,
			"bad",
		},
	}

	for _, test := range tests {
		actual := EvilIPs.lookup(test.ip)
		if actual != test.want {
			t.Errorf("test %s failed actual %s != want %s", test.name, actual, test.want)
		}
	}
}
