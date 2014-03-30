// rgip: restful geoip lookup service
package main

import (
	"encoding/csv"
	"encoding/json"
	"expvar"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/abh/geoip"
)

var Statistics = struct {
	Requests *expvar.Int
	Errors   *expvar.Int
}{
	Requests: expvar.NewInt("requests"),
	Errors:   expvar.NewInt("errors"),
}

type City struct {
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	DMACode     int     `json:"dma_code"` // not supported by Go bindings
	Latitude    float32 `json:"latitude"`
	Longitude   float32 `json:"longitude"`
	MetroCode   int     `json:"metro_code"` // == DMACode, not supported by Go bindings
	Region      string  `json:"region"`
	RegionName  string  `json:"region_name"`

	AreaCode int `json:"area_code"`
}

type IPInfo struct {
	IP       string `json:"ip"`
	City     `json:"city"`
	ISP      string `json:"isp"`
	NetSpeed string `json:"netspeed"`
	UFI      struct {
		GuessedUFI int `json:"guessed_ufi"`
	} `json:"ufi"`
	NextHop string `json:"next_hop_ip"`
}

var gcity, gspeed, gisp *geodb

type geodb struct {
	db *geoip.GeoIP
	sync.Mutex
}

func (g *geodb) load(dataDir, file string) error {
	fname := path.Join(dataDir, file)
	db, err := geoip.Open(fname)
	if err != nil {
		log.Printf("error loading %s/%s: %s", dataDir, file, err)
		return err
	}

	g.Lock()
	g.db = db
	g.Unlock()
	return nil
}

func (g *geodb) GetNetSpeed(ip string) string {
	g.Lock()
	defer g.Unlock()
	speed, _ /* netmask */ := g.db.GetName(ip)
	if speed == "" {
		return "Unknown"
	}

	return speed
}

func (g *geodb) GetOrg(ip string) string {
	g.Lock()
	defer g.Unlock()
	return g.db.GetOrg(ip)
}

func (g *geodb) GetRecord(ip string) *geoip.GeoIPRecord {
	g.Lock()
	defer g.Unlock()
	return g.db.GetRecord(ip)
}

type ipRange struct {
	rangeFrom, rangeTo uint32
	data               int
}

type ipRanges struct {
	ranges []ipRange
	sync.RWMutex
}

var ufis, nexthops *ipRanges

type converr struct {
	err error
}

func (c *converr) check(s string, f func(string) (int, error)) int {
	i, e := f(s)
	if e != nil {
		c.err = e
		return 0
	}
	return i
}

func (ipr *ipRanges) load(fname string, transform func(string) (int, error)) error {

	var f io.ReadCloser

	f, err := os.Open(fname)
	defer f.Close()

	if err != nil {
		log.Println("can't open ip ranges: ", err)
		return err
	}

	svr := csv.NewReader(f)

	var ips []ipRange

	prevIP := -1

	for {
		r, err := svr.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Println("error reading CSV: ", err)
			return err
		}

		var ipFrom, ipTo, data int

		var convert converr
		if len(r) < 3 {
			ipFrom = prevIP + 1
			ipTo = convert.check(r[0], strconv.Atoi)
			data = convert.check(r[1], transform)
			prevIP = ipTo
		} else {
			ipFrom = convert.check(r[0], strconv.Atoi)
			ipTo = convert.check(r[1], strconv.Atoi)
			data = convert.check(r[2], transform)
		}

		if convert.err != nil {
			log.Printf("error parsing %v: %s", r, err)
			return convert.err
		}

		ips = append(ips, ipRange{rangeFrom: uint32(ipFrom), rangeTo: uint32(ipTo), data: data})
	}

	ipr.Lock()
	ipr.ranges = ips
	ipr.Unlock()
	return nil
}

func (ipr *ipRanges) lookupRange(ip32 uint32) int {

	ipr.RLock()
	defer ipr.RUnlock()

	idx := sort.Search(len(ipr.ranges), func(i int) bool { return ip32 <= ipr.ranges[i].rangeTo })

	if idx != -1 && ipr.ranges[idx].rangeFrom <= ip32 && ip32 <= ipr.ranges[idx].rangeTo {
		return ipr.ranges[idx].data
	}

	return 0
}

func lookupHandler(w http.ResponseWriter, r *http.Request) {

	Statistics.Requests.Add(1)

	// split path for IP
	args := strings.Split(r.URL.Path, "/")
	// strip entry for "/lookup/"
	args = args[2:]

	if len(args) != 1 {
		Statistics.Errors.Add(1)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ip := args[0]

	var netip net.IP
	if netip = net.ParseIP(ip); netip == nil {
		Statistics.Errors.Add(1)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ipinfo := IPInfo{
		IP: ip,
	}

	if gspeed != nil {
		ipinfo.NetSpeed = gspeed.GetNetSpeed(ip)
	}

	if gisp != nil {
		ipinfo.ISP = gisp.GetOrg(ip)
		// catch unknown org?
	}

	if ip4 := netip.To4(); ip4 != nil && (ufis != nil || nexthops != nil) {
		ip32 := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])

		if ufis != nil {
			ipinfo.UFI.GuessedUFI = ufis.lookupRange(ip32)
		}

		if nexthops != nil {
			nexthop := uint32(nexthops.lookupRange(ip32))
			ipinfo.NextHop = net.IPv4(byte(nexthop>>24), byte(nexthop>>16), byte(nexthop>>8), byte(nexthop)).String()
		}
	}

	if record := gcity.GetRecord(ip); record != nil {
		ipinfo.City.City = record.City
		ipinfo.CountryCode = strings.ToLower(record.CountryCode)
		ipinfo.Latitude = record.Latitude
		ipinfo.Longitude = record.Longitude
		ipinfo.Region = record.Region
		ipinfo.RegionName = geoip.GetRegionName(record.CountryCode, record.Region)

		ipinfo.AreaCode = record.AreaCode
	}

	// check RobotIP
	// check EvilISP

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfo)
}

type errIPParse string

func (ip errIPParse) Error() string {
	return fmt.Sprintf("bad ip address: %s", ip)
}

func loadDataFiles(lite bool, datadir, ufi, nexthop string) error {

	var err error

	if lite {
		e := gcity.load(datadir, "GeoLiteCity.dat")
		if e != nil {
			err = e
		}
	} else {
		e := gcity.load(datadir, "GeoIPCity.dat")
		if e != nil {
			err = e
		}
		e = gspeed.load(datadir, "GeoIPNetSpeed.dat")
		if e != nil {
			err = e
		}

		e = gisp.load(datadir, "GeoIPISP.dat")
		if e != nil {
			err = e
		}
	}

	if ufi != "" {
		e := ufis.load(ufi, strconv.Atoi)
		if e != nil {
			log.Printf("unable to load %s: %s", ufi, err)
			err = e
		}
	}

	if nexthop != "" {
		e := nexthops.load(nexthop, func(s string) (int, error) {
			netip := net.ParseIP(s)
			if netip == nil {
				return 0, errIPParse(s)
			}

			ip4 := netip.To4()
			ip32 := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
			return int(ip32), nil
		})
		if e != nil {
			log.Printf("unable to load %s: %s", nexthop, err)
			err = e
		}
	}
	return err
}

func main() {

	dataDir := flag.String("datadir", "", "Directory containing GeoIP data files")
	ufi := flag.String("ufi", "", "File containing iprange-to-UFI mappings")
	nexthop := flag.String("nexthop", "", "File containing next-hop mappings")
	lite := flag.Bool("lite", false, "Load only GeoLiteCity.dat")

	flag.Parse()

	gcity = new(geodb)
	if !*lite {
		gspeed = new(geodb)
		gisp = new(geodb)
	}

	if *ufi != "" {
		ufis = new(ipRanges)
	}

	if *nexthop != "" {
		nexthops = new(ipRanges)
	}

	err := loadDataFiles(*lite, *dataDir, *ufi, *nexthop)
	if err != nil {
		log.Fatal("can't load data files: ", err)

	}

	// start the sighup reload config handler
	go func() {
		sigs := make(chan os.Signal)
		signal.Notify(sigs, syscall.SIGHUP)

		for {
			<-sigs
			log.Println("Attempting to reload data files")
			// TODO(dgryski): run this in a goroutine and catch panics()?
			err := loadDataFiles(*lite, *dataDir, *ufi, *nexthop)
			if err != nil {
				// don't log err here, we've already done it in loadDataFiles
				log.Println("failed to load some data files")
			} else {
				log.Println("All data files reloaded successfully")
			}
		}

	}()

	http.HandleFunc("/lookup/", lookupHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
