package main

import (
	"testing"
	"time"
)

func TestLookupBadIp(t *testing.T) {
	unix_limit := time.Date(2038, time.January, 19, 3, 14, 8, 0, time.UTC)
	var badIps badIpRanges
	badIps.ranges = []badIpRange{
		{387534209, 387534209, BadIPRecord{"expired", time.Now()}},
		{387534210, 387534213, BadIPRecord{"bad", unix_limit}},
		{387534214, 387534214, BadIPRecord{"badder", unix_limit}},
	}
	EvilIPs := EvilIPList{
		badIps,
		time.Now(),
	}

	tests := []struct {
		ip   uint32
		want string
	}{
		{
			387534209,
			"",
		},
		{
			387534214,
			"badder",
		},
		{
			387534212,
			"bad",
		},
	}

	for _, test := range tests {
		actual := EvilIPs.lookup(test.ip)
		if actual != test.want {
			t.Errorf("actual %s != want %s", actual, test.want)
		}
	}
}
