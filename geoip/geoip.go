// Package geoip provides a thin wrapper around libGeoIP for looking up
// geographical information about IP addresses.
package geoip

import (
	"runtime"
	"unsafe"
)

// #cgo LDFLAGS: -lGeoIP
// #include "GeoIP.h"
// #include "GeoIPCity.h"
import "C"

// CachingStrategy determines what data libGeoIP will cache.
type CachingStrategy int

const (
	// CacheDefault caches no data.
	CacheDefault CachingStrategy = C.GEOIP_STANDARD

	// CacheAll caches all data in memory.
	CacheAll CachingStrategy = C.GEOIP_MEMORY_CACHE

	// CacheMRU caches the most recently used data in memory.
	CacheMRU CachingStrategy = C.GEOIP_INDEX_CACHE
)

// Options are the set of options provided by libGeoIP.
type Options struct {
	Caching        CachingStrategy // Caching determines what data will be cached.
	ReloadOnUpdate bool            // ReloadOnUpdate will watch the data files for updates.
	UseMMap        bool            // UseMMap enables MMAP for the data files.
}

func (o Options) bitmask() int32 {
	v := int32(o.Caching)

	if o.ReloadOnUpdate {
		v |= C.GEOIP_CHECK_CACHE
	}

	if o.UseMMap {
		v |= C.GEOIP_MMAP_CACHE
	}

	return v
}

// DefaultOptions caches no data, reloads on updates, and uses MMAP.
var DefaultOptions = &Options{
	Caching:        CacheDefault,
	ReloadOnUpdate: true,
	UseMMap:        true,
}

// Record is a GeoIP record.
type Record struct {
	CountryCode   string  // CountryCode is a two-letter country code.
	CountryCode3  string  // CountryCode3 is a three-letter country code.
	CountryName   string  // CountryName is the name of the country.
	Region        string  // Region is the geographical region of the location.
	City          string  // City is the name of the city.
	PostalCode    string  // PostalCode is the location's postal code.
	Latitude      float64 // Latitude is the location's latitude.
	Longitude     float64 // Longitude is the location's longitude.
	AreaCode      int     // AreaCode is the location's area code.
	ContinentCode string  // ContinentCode is the location's continent.
}

// A Database is a GeoIP database.
type Database struct {
	g *C.GeoIP
}

// Open returns an open DB instance of the given .dat file. The result *must* be
// closed, or memory will leak.
func Open(filename string, opts *Options) (*Database, error) {
	if opts == nil {
		opts = DefaultOptions
	}

	cs := C.CString(filename)
	defer C.free(unsafe.Pointer(cs))

	g, err := C.GeoIP_open(cs, C.int(opts.bitmask()))
	if err != nil {
		return nil, err
	}
	C.GeoIP_set_charset(g, C.GEOIP_CHARSET_UTF8)

	db := &Database{g: g}
	runtime.SetFinalizer(db, func(db *Database) {
		_ = db.Close()
	})
	return db, nil
}

// Lookup returns a GeoIP Record for the given IP address. If libGeoIP is >
// 1.5.0, this is thread-safe.
func (db *Database) Lookup(ip string) *Record {
	cs := C.CString(ip)
	defer C.free(unsafe.Pointer(cs))

	r := C.GeoIP_record_by_addr(db.g, cs)
	if r == nil {
		return nil
	}
	defer C.GeoIPRecord_delete(r)

	return &Record{
		CountryCode:   C.GoString(r.country_code),
		CountryCode3:  C.GoString(r.country_code3),
		CountryName:   C.GoString(r.country_name),
		Region:        C.GoString(r.region),
		City:          C.GoString(r.city),
		PostalCode:    C.GoString(r.postal_code),
		Latitude:      float64(r.latitude),
		Longitude:     float64(r.longitude),
		AreaCode:      int(r.area_code),
		ContinentCode: C.GoString(r.continent_code),
	}
}

func (db *Database) GetName(ip string) (name string, netmask int) {
	cip := C.CString(ip)
	defer C.free(unsafe.Pointer(cip))
	var gl C.GeoIPLookup
	cname := C.GeoIP_name_by_addr_gl(db.g, cip, &gl)
	if cname == nil {
		return "", 0
	}

	name = C.GoString(cname)
	netmask = int(gl.netmask)
	C.free(unsafe.Pointer(cname))
	return name, netmask
}

func (db *Database) GetNameV6(ip string) (name string, netmask int) {
	cip := C.CString(ip)
	defer C.free(unsafe.Pointer(cip))
	var gl C.GeoIPLookup
	cname := C.GeoIP_name_by_addr_v6_gl(db.g, cip, &gl)
	if cname == nil {
		return "", 0
	}

	name = C.GoString(cname)
	netmask = int(gl.netmask)
	C.free(unsafe.Pointer(cname))
	return name, netmask
}

// Close releases the resources allocated by the database.
func (db *Database) Close() error {
	if db.g != nil {
		C.GeoIP_delete(db.g)
	}
	db.g = nil
	return nil
}

func GetTimeZone(country, region string) string {

	ccountry := C.CString(country)
	defer C.free(unsafe.Pointer(ccountry))

	cregion := C.CString(region)
	defer C.free(unsafe.Pointer(cregion))

	ctz := C.GeoIP_time_zone_by_country_and_region(ccountry, cregion)
	if ctz == nil {
		return ""
	}

	// static string
	tz := C.GoString(ctz)
	return tz
}

func GetRegionName(countryCode, regionCode string) string {

	cccode := C.CString(countryCode)
	defer C.free(unsafe.Pointer(cccode))

	crcode := C.CString(regionCode)
	defer C.free(unsafe.Pointer(crcode))

	region := C.GeoIP_region_name_by_code(cccode, crcode)
	if region == nil {
		return ""
	}

	// static string
	return C.GoString(region)
}
