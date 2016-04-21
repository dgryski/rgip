// Command rgip is a restful geoip lookup service
package main

import (
	"encoding/json"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dgryski/rgip/geoip"
	"github.com/dgryski/rgip/mlog"
	"github.com/facebookgo/grace/gracehttp"
	olc "github.com/google/open-location-code/go"
	geoip2 "github.com/oschwald/geoip2-golang"
	maxminddb "github.com/oschwald/maxminddb-golang"
	"github.com/peterbourgon/g2g"
	"github.com/pierrre/geohash"
)

// Metrics tracks metrics for this server
var Metrics = struct {
	Requests *expvar.Int
	Errors   *expvar.Int
}{
	Requests: expvar.NewInt("requests"),
	Errors:   expvar.NewInt("errors"),
}

var BuildVersion = "(development version)"

// City is a maxmind GeoIP city response
type City struct {
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	Latitude    float32 `json:"latitude"`
	Longitude   float32 `json:"longitude"`
	Region      string  `json:"region,omitempty"`
	RegionName  string  `json:"region_name,omitempty"`
	PostalCode  string  `json:"postal_code,omitempty"`
	AreaCode    int     `json:"area_code"`
	TimeZone    string  `json:"time_zone,omitempty"`
}

// IPInfo is the response type for the server
type IPInfo struct {
	IP       string `json:"ip"`
	City     `json:"city"`
	ISP      string `json:"isp"`
	NetSpeed string `json:"netspeed"`
	UFI      struct {
		GuessedUFI int32 `json:"guessed_ufi"`
	} `json:"ufi"`
	IPStatus string `json:"ip_status,omitempty"`
	GeoHash  string `json:"geohash,omitempty"`
	OLC      string `json:"olc,omitempty"`
}

// these are connections to the different maxmind geoip databases
var (
	gcity  *geodb
	gspeed *geodb
	gisp   *geodb
)

type geodb struct {
	db *geoip.Database
	sync.RWMutex
}

func (g *geodb) load(dataDir, file string) error {
	fname := path.Join(dataDir, file)
	opts := *geoip.DefaultOptions // copy
	db, err := geoip.Open(fname, &opts)
	if err != nil {
		mlog.Printf("error loading %s/%s: %s", dataDir, file, err)
		return err
	}

	g.Lock()
	g.db = db
	g.Unlock()
	return nil
}

func (g *geodb) GetNetSpeed(ip string) string {
	g.RLock()
	speed, _ /* netmask */ := g.db.GetName(ip)
	g.RUnlock()
	if speed == "" {
		return "Unknown"
	}

	return speed
}

func (g *geodb) GetNetSpeedV6(ip string) string {
	g.RLock()
	speed, _ /* netmask */ := g.db.GetNameV6(ip)
	g.RUnlock()
	if speed == "" {
		return "Unknown"
	}

	return speed
}

func (g *geodb) GetName(ip string) string {
	g.RLock()
	name, _ := g.db.GetName(ip)
	g.RUnlock()
	return name
}

func (g *geodb) GetNameV6(ip string) string {
	g.RLock()
	name, _ := g.db.GetNameV6(ip)
	g.RUnlock()
	return name
}

func (g *geodb) GetRecord(ip string) *geoip.Record {
	g.RLock()
	r := g.db.Lookup(ip)
	g.RUnlock()
	return r
}

var (
	g2city *geoip2.Reader
	g2ufi  *maxminddb.Reader
)

// ufis maps IP addresses to UFIs
var ufis *ipRanges

var errParseError = errors.New("ipinfo: parse error")

func lookupIPInfo(ip string) (IPInfo, error) {
	var netip net.IP
	if netip = net.ParseIP(ip); netip == nil {
		return IPInfo{}, errParseError
	}

	ipinfo := IPInfo{
		IP: ip,
	}

	if gspeed != nil {
		if netip.To4() != nil {
			ipinfo.NetSpeed = gspeed.GetNetSpeed(ip)
		} else {
			ipinfo.NetSpeed = gspeed.GetNetSpeedV6(ip)
		}
	}

	if gisp != nil {
		if netip.To4() != nil {
			ipinfo.ISP = gisp.GetName(ip)
		} else {
			ipinfo.ISP = gisp.GetNameV6(ip)
		}
		// catch unknown org?
	}

	var ip32 uint32

	if ip4 := netip.To4(); ip4 != nil {
		ip32 = uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
	}

	if ufis != nil {
		ufi, ok := ufis.lookup(ip32)
		if ok {
			ipinfo.UFI.GuessedUFI = ufi
		}
	}

	if g2ufi != nil {
		ufi, err := mmdbIP2UFI(netip)
		if err != nil {
			mlog.Println("mmdb error: ", err)
		}
		ipinfo.UFI.GuessedUFI = ufi
	}

	if record := gcity.GetRecord(ip); record != nil {
		ipinfo.City.City = record.City
		ipinfo.CountryCode = strings.ToLower(record.CountryCode)
		ipinfo.Latitude = float32(record.Latitude)
		ipinfo.Longitude = float32(record.Longitude)
		ipinfo.Region = record.Region
		ipinfo.RegionName = geoip.GetRegionName(record.CountryCode, record.Region)
		ipinfo.City.TimeZone = geoip.GetTimeZone(record.CountryCode, record.Region)
		ipinfo.City.PostalCode = record.PostalCode
		ipinfo.AreaCode = record.AreaCode
	}

	ipinfo.GeoHash = geohash.Encode(float64(ipinfo.Latitude), float64(ipinfo.Longitude), 10)
	ipinfo.OLC = olc.Encode(float64(ipinfo.Latitude), float64(ipinfo.Longitude), 10)

	// TODO(dgryski): check EvilISP

	return ipinfo, nil
}

const contentTypeJSON = `application/json; charset=utf-8`

func lookupHandler(w http.ResponseWriter, r *http.Request) {

	Metrics.Requests.Add(1)

	// split path for IP
	args := strings.Split(r.URL.Path, "/")
	// strip entry for "/lookup/"
	args = args[2:]

	if len(args) != 1 {
		Metrics.Errors.Add(1)
		mlog.Println("error parsing request path:", r.URL)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ip := args[0]
	ipinfo, err := lookupIPInfo(ip)
	if err != nil {
		Metrics.Errors.Add(1)
		mlog.Println("error during lookup:", ip)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfo)
}

func lookupsHandler(w http.ResponseWriter, r *http.Request) {

	Metrics.Requests.Add(1)

	// split path for IP
	args := strings.Split(r.URL.Path, "/")
	// strip entry for "/lookup/"
	args = args[2:]

	if len(args) != 1 {
		Metrics.Errors.Add(1)
		mlog.Println("error parsing request path:", r.URL)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ipinfos := make(map[string]IPInfo)

	for _, ip := range strings.Split(args[0], ",") {
		ipinfo, err := lookupIPInfo(ip)
		if err != nil {
			Metrics.Errors.Add(1)
			mlog.Println("error during lookup:", ip)
			ipinfos[ip] = IPInfo{IPStatus: "ParseError"}
		} else {
			ipinfos[ip] = ipinfo
		}
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfos)
}

var errParseIP = errors.New("bad ip: parse error")

func lookupIPInfo2(ip string) (*geoip2.City, error) {
	netip := net.ParseIP(ip)
	if netip == nil {
		return nil, errParseIP
	}

	return g2city.City(netip)
}

func mmdbIP2UFI(netip net.IP) (int32, error) {
	var onlyUFI struct {
		UFI int32 `maxminddb:"ufi"`
	}

	err := g2ufi.Lookup(netip, &onlyUFI)
	if err != nil {
		return 0, err
	}

	return onlyUFI.UFI, nil
}

func lookup2Handler(w http.ResponseWriter, r *http.Request) {

	Metrics.Requests.Add(1)

	// split path for IP
	args := strings.Split(r.URL.Path, "/")
	// strip entry for "/lookup/"
	args = args[2:]

	if len(args) != 1 {
		Metrics.Errors.Add(1)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ip := args[0]
	ipinfo, err := lookupIPInfo2(ip)
	if err != nil {
		Metrics.Errors.Add(1)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfo)
}

type IP2Info struct {
	*geoip2.City
	IPStatus string
}

func lookups2Handler(w http.ResponseWriter, r *http.Request) {

	Metrics.Requests.Add(1)

	// split path for IP
	args := strings.Split(r.URL.Path, "/")
	// strip entry for "/lookups2/"
	args = args[2:]

	if len(args) != 1 {
		Metrics.Errors.Add(1)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ipinfos := make(map[string]IP2Info)

	for _, ip := range strings.Split(args[0], ",") {
		ipinfo, err := lookupIPInfo2(ip)
		if err != nil {
			Metrics.Errors.Add(1)
			ipinfos[ip] = IP2Info{IPStatus: "ParseError"}
		} else {
			ipinfos[ip] = IP2Info{City: ipinfo}
		}
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfos)
}

func loadDataFiles(lite bool, datadir, ufi string, isbinary bool) error {

	var err error

	if lite {
		e := gcity.load(datadir, "GeoLiteCity.dat")
		if e != nil {
			err = e
		}
	} else {
		e := gcity.load(datadir, "GeoIPCity.dat") // This IP is in "Amsterdam"
		if e != nil {
			err = e
		}
		e = gspeed.load(datadir, "GeoIPNetSpeed.dat") // This IP belongs to Vodafone and it's a mobile thing, or it's Comcast / DSL..
		if e != nil {
			err = e
		}

		e = gisp.load(datadir, "GeoIPISP.dat") // This is "Time Warner" or "AOL"
		if e != nil {
			err = e
		}
	}

	if ufi != "" {
		// ip -> ufi mapping
		ranges, e := loadIPRanges(ufi, isbinary)
		if e != nil {
			mlog.Printf("unable to load %s: %s", ufi, e)
			err = e
		} else {
			ufis.Lock()
			ufis.ranges = ranges
			ufis.Unlock()
		}
	}

	return err
}

func saveBinary(fname string, ranges []ipRange) {
	fname = fmt.Sprintf("%s.bin", fname)
	log.Println("writing", len(ranges), "items to", fname)
	file, err := os.Create(fname)
	if err != nil {
		log.Println("can't open file: ", fname, err)
		return
	}

	defer file.Close()
	err = writeBinary(file, ranges)
	if err != nil {
		log.Fatal("saveBinary failed", err)
	}
}

func main() {

	dataDir := flag.String("datadir", "", "Directory containing GeoIP data files")
	data2Dir := flag.String("data2dir", "", "Directory containing GeoIP2 data files")
	ufi := flag.String("ufi", "", "File containing iprange-to-UFI mappings")
	ufi2 := flag.String("ufi2", "", "File containing iprange-to-UFI mappings mmdb")
	isbinary := flag.Bool("isbinary", false, "load iprange-to-UFI mapping as a binary file instead of parsing it as CSV")
	convert := flag.Bool("convert", false, "Parse iprange-to-UFI CSV and save it as Memory-map files")
	lite := flag.Bool("lite", false, "Load only GeoLiteCity.dat")
	port := flag.Int("p", 8080, "port")

	flag.Parse()

	if *data2Dir != "" {
		var err error
		g2city, err = geoip2.Open(*data2Dir + "/GeoLite2-City.mmdb")
		if err != nil {
			mlog.Fatal("error loading geoip2:", err)
		}
	}

	if *ufi2 != "" {
		var err error
		g2ufi, err = maxminddb.Open(*ufi2)
		if err != nil {
			mlog.Fatal("error loading ip2ufi:", err)
		}
	}

	if *ufi != "" {
		ufis = new(ipRanges)
		if *convert {
			mlog.Println("loading iprange-to-UFI CSV")
			ranges, e := loadIPRanges(*ufi, *isbinary)
			if e == nil {
				saveBinary(*ufi, ranges)
			}
			return
		}
	}

	expvar.NewString("BuildVersion").Set(BuildVersion)

	// TODO(dgryski): add proper log output
	mlog.Println("rgip starting", BuildVersion)

	gcity = new(geodb)
	if !*lite {
		gspeed = new(geodb)
		gisp = new(geodb)
	}

	err := loadDataFiles(*lite, *dataDir, *ufi, *isbinary)
	if err != nil {
		mlog.Fatal("error loading data files: ", err)
	}

	// start the reload-on-change handler
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGHUP)

		for range sigs {
			mlog.Println("Attempting to reload data files")
			// TODO(dgryski): run this in a goroutine and catch panics()?
			err := loadDataFiles(*lite, *dataDir, *ufi, *isbinary)
			if err != nil {
				// don't log err here, we've already done it in loadDataFiles
				mlog.Println("failed to load some data files")
			} else {
				mlog.Println("All data files reloaded successfully")
			}
		}
	}()

	if host := os.Getenv("GRAPHITEHOST") + ":" + os.Getenv("GRAPHITEPORT"); host != ":" {
		// register our metrics with graphite
		graphite := g2g.NewGraphite(host, 60*time.Second, 10*time.Second)

		hostname, _ := os.Hostname()
		hostname = strings.Replace(hostname, ".", "_", -1)

		graphite.Register(fmt.Sprintf("http.rgip.%s.requests", hostname), Metrics.Requests)
		graphite.Register(fmt.Sprintf("http.rgip.%s.errors", hostname), Metrics.Errors)
	}

	http.HandleFunc("/lookup/", lookupHandler)
	http.HandleFunc("/lookups/", lookupsHandler)

	http.HandleFunc("/lookup2/", lookup2Handler)
	http.HandleFunc("/lookups2/", lookups2Handler)

	if p := os.Getenv("PORT"); p != "" {
		*port, err = strconv.Atoi(p)
		if err != nil {
			mlog.Fatal("unable to parse port number:", err)
		}
	}
	mlog.Println("listening on port", *port)

	s := &http.Server{
		Addr:    ":" + strconv.Itoa(*port),
		Handler: nil,
	}

	gracehttp.Serve(s)
}
