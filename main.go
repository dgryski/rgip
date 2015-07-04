// rgip: restful geoip lookup service
// Someday: IPv6
package main

import (
	"database/sql"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dgryski/rgip/geoip"
	"github.com/facebookgo/grace/gracehttp"
	_ "github.com/mattn/go-sqlite3"
)

// Statistics tracks metrics for this server
var Statistics = struct {
	Requests *expvar.Int
	Errors   *expvar.Int
}{
	Requests: expvar.NewInt("requests"),
	Errors:   expvar.NewInt("errors"),
}

// City is a maxmind GeoIP city response
type City struct {
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	Latitude    float32 `json:"latitude"`
	Longitude   float32 `json:"longitude"`
	Region      string  `json:"region"`
	RegionName  string  `json:"region_name"`
	PostalCode  string  `json:"postal_code"`
	AreaCode    int     `json:"area_code"`
	TimeZone    string  `json:"time_zone"`
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
	IPStatus string `json:"ip_status"`
}

// these are connections to the different maxmind geoip databases
var (
	gcity  *geodb
	gspeed *geodb
	gisp   *geodb
)

type geodb struct {
	db *geoip.Database
	sync.Mutex
}

func (g *geodb) load(dataDir, file string) error {
	fname := path.Join(dataDir, file)
	opts := *geoip.DefaultOptions // copy
	opts.NoLocks = true
	db, err := geoip.Open(fname, &opts)
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

func (g *geodb) GetNetSpeedV6(ip string) string {
	g.Lock()
	defer g.Unlock()
	speed, _ /* netmask */ := g.db.GetNameV6(ip)
	if speed == "" {
		return "Unknown"
	}

	return speed
}

func (g *geodb) GetName(ip string) string {
	g.Lock()
	defer g.Unlock()
	name, _ := g.db.GetName(ip)
	return name
}

func (g *geodb) GetNameV6(ip string) string {
	g.Lock()
	defer g.Unlock()
	name, _ := g.db.GetNameV6(ip)
	return name
}

func (g *geodb) GetRecord(ip string) *geoip.Record {
	g.Lock()
	defer g.Unlock()
	return g.db.Lookup(ip)
}

// ufis maps IP addresses to UFIs
var ufis *ipRanges

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

	ipinfo.IPStatus = evilIPs.lookup(ip32)

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

	// check EvilISP

	return ipinfo, nil
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
	ipinfo, err := lookupIPInfo(ip)
	if err != nil {
		Statistics.Errors.Add(1)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfo)
}

func lookupsHandler(w http.ResponseWriter, r *http.Request) {

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

	ipinfos := make(map[string]IPInfo)

	for _, ip := range strings.Split(args[0], ",") {
		ipinfo, err := lookupIPInfo(ip)
		if err != nil {
			ipinfos[ip] = IPInfo{IPStatus: "ParseError"}
		} else {
			ipinfos[ip] = ipinfo
		}
	}

	w.Header().Set("Content-Type", "application/json")
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
			log.Printf("unable to load %s: %s", ufi, e)
			err = e
		} else {
			ufis.Lock()
			ufis.ranges = ranges
			ufis.Unlock()
		}
	}

	return err
}

// evilIPs hosts the list of 'bad' IPs and their statuses
var evilIPs evilIPList

func loadEvilIP(db *sql.DB) (badIPRangeList, error) {

	// TODO(dgryski); check if data *needs* reloading?
	// TODO(dgryski): current_date is sqlite-ism

	rows, err := db.Query("select ip, subnet, status, expires from EvilIP where expires > current_date")
	if err != nil {
		log.Println("error querying: ", err)
		return nil, err
	}

	defer rows.Close()

	var ranges badIPRangeList

	for rows.Next() {
		var ip uint32
		var subnet uint
		var status string
		var expires string

		err := rows.Scan(&ip, &subnet, &status, &expires)
		if err != nil {
			log.Println("error scanning: ", err)
			return nil, err
		}

		mask := uint32(1<<(32-subnet)) - 1
		ipmin := ip & ^mask
		ipmax := ip | mask

		expireTime, err := time.Parse("2006-01-02", expires)
		badIP := badIPRecord{
			status:  status,
			expires: expireTime,
		}

		ranges = append(ranges, badIPRange{rangeFrom: ipmin, rangeTo: ipmax, data: badIP})
	}
	err = rows.Err()
	if err != nil {
		log.Println("error from rows:", err)
		return nil, err
	}

	// TODO(dgryski): ensure the data has no overlapping ranges

	sort.Sort(ranges)

	return ranges, nil
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
	ufi := flag.String("ufi", "", "File containing iprange-to-UFI mappings")
	isbinary := flag.Bool("isbinary", false, "load iprange-to-UFI mapping as a binary file instead of parsing it as CSV")
	convert := flag.Bool("convert", false, "Parse iprange-to-UFI CSV and save it as Memory-map files")
	lite := flag.Bool("lite", false, "Load only GeoLiteCity.dat")
	// This is what RobotIP is going to become
	evilip := flag.String("evilip", "", "Watch EvilIP table for changes")
	port := flag.Int("p", 8080, "port")

	flag.Parse()

	if *ufi != "" {
		ufis = new(ipRanges)
		if *convert {
			log.Println("loading iprange-to-UFI CSV")
			ranges, e := loadIPRanges(*ufi, *isbinary)
			if e == nil {
				saveBinary(*ufi, ranges)
			}

			return
		}
	}

	log.Println("rgip starting")

	gcity = new(geodb)
	if !*lite {
		gspeed = new(geodb)
		gisp = new(geodb)
	}

	err := loadDataFiles(*lite, *dataDir, *ufi, *isbinary)
	if err != nil {
		log.Fatal("can't load data files: ", err)

	}

	var evilipdb *sql.DB

	if *evilip != "" {
		var err error
		evilipdb, err = sql.Open("sqlite3", *evilip)
		if err != nil {
			log.Fatal(err)
		}
		ranges, err := loadEvilIP(evilipdb)
		if err != nil {
			log.Fatal(err)
		} else {
			evilIPs.Lock()
			evilIPs.ranges = ranges
			evilIPs.Unlock()
		}
	}

	// start the reload-on-change handler
	go func() {
		sigs := make(chan os.Signal)
		signal.Notify(sigs, syscall.SIGHUP)

		var minute <-chan time.Time

		if *evilip != "" {
			minute = time.Tick(time.Minute)
		}

		for {
			select {

			case <-sigs:
				log.Println("Attempting to reload data files")
				// TODO(dgryski): run this in a goroutine and catch panics()?
				err := loadDataFiles(*lite, *dataDir, *ufi, *isbinary)
				if err != nil {
					// don't log err here, we've already done it in loadDataFiles
					log.Println("failed to load some data files")
				} else {
					log.Println("All data files reloaded successfully")
				}

			case <-minute:
				log.Println("reloading EvilIP data")
				ranges, err := loadEvilIP(evilipdb)
				if err != nil {
					// don't log err here, we've already done it in loadDataFiles
					log.Println("failed to reload EvilIP data")
				} else {
					// assign ranges to evilips
					log.Println("EvilIP data reloaded")
					evilIPs.Lock()
					evilIPs.ranges = ranges
					evilIPs.Unlock()
				}
			}
		}

	}()

	http.HandleFunc("/lookup/", lookupHandler)
	http.HandleFunc("/lookups/", lookupsHandler)

	if p := os.Getenv("PORT"); p != "" {
		*port, err = strconv.Atoi(p)
		if err != nil {
			log.Fatal("unable to parse port number:", err)
		}
	}
	log.Println("listening on port", *port)

	s := &http.Server{
		Addr:    ":" + strconv.Itoa(*port),
		Handler: nil,
	}

	gracehttp.Serve(s)
}
