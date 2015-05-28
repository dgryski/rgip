package geoip

import (
	"flag"
	"fmt"
	"reflect"
	"testing"
)

func Example() {
	g, err := Open(*dbFile, nil)
	if err != nil {
		panic(err)
	}
	defer g.Close()

	fmt.Printf("%#v\n", g.Lookup("24.24.24.24"))

	// Output:
	// &geoip.Record{CountryCode:"US", CountryCode3:"USA", CountryName:"United States", Region:"NY", City:"Deer Park", PostalCode:"11729", Latitude:40.762699127197266, Longitude:-73.32270050048828, AreaCode:631, ContinentCode:"NA"}
}

func TestOpen(t *testing.T) {
	g, err := Open(*dbFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
}

func TestOpenBadFile(t *testing.T) {
	g, err := Open("/example/usr/GeoIPCity.dat", nil)
	if g != nil {
		t.Fatalf("Was %v, but expected nil", g)
	}

	if err == nil {
		t.Fatalf("Was nil, but expected an error")
	}
}

func TestLookup(t *testing.T) {
	g, err := Open(*dbFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	actual := g.Lookup("24.24.24.24")
	expected := &Record{
		CountryCode:   "US",
		CountryCode3:  "USA",
		CountryName:   "United States",
		Region:        "NY",
		City:          "Deer Park",
		PostalCode:    "11729",
		Latitude:      40.762699127197266,
		Longitude:     -73.32270050048828,
		AreaCode:      631,
		ContinentCode: "NA",
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Was %#v, but expected %#v", actual, expected)
	}
}

func TestLookupNoLocks(t *testing.T) {
	g, err := Open(*dbFile, &Options{NoLocks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	actual := g.Lookup("24.24.24.24")
	expected := &Record{
		CountryCode:   "US",
		CountryCode3:  "USA",
		CountryName:   "United States",
		Region:        "NY",
		City:          "Deer Park",
		PostalCode:    "11729",
		Latitude:      40.762699127197266,
		Longitude:     -73.32270050048828,
		AreaCode:      631,
		ContinentCode: "NA",
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Was %#v, but expected %#v", actual, expected)
	}
}

func BenchmarkLookup(b *testing.B) {
	g, err := Open(*dbFile, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer g.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		g.Lookup("24.24.24.24")
	}
}

func BenchmarkLookupParallel(b *testing.B) {
	g, err := Open(*dbFile, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer g.Close()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Lookup("24.24.24.24")
		}
	})
}

var (
	dbFile = flag.String("db_file", "/usr/local/var/GeoIP/GeoIPCity.dat", "GeoIP database")
)
