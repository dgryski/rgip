// rgip: restful geoip lookup service
package main

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"expvar"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

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
	MetroCode   int     `json:"metro_code""` // == DMACode, not supported by Go bindings
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

var gcity, gspeed, gisp *geoip.GeoIP

func opendat(dataDir string, dat string) *geoip.GeoIP {
	fname := path.Join(dataDir, dat)
	g, err := geoip.Open(fname)
	if err != nil {
		log.Fatalf("unable to open %s: %s", fname, err)
	}

	return g
}

type ipRange struct {
	rangeFrom, rangeTo uint32
	data               int
}

type ipRanges []ipRange

func (ipr *ipRanges) Len() int           { return len(*ipr) }
func (ipr *ipRanges) Less(i, j int) bool { return (*ipr)[i].rangeTo < (*ipr)[j].rangeTo }
func (ipr *ipRanges) Swap(i, j int)      { (*ipr)[i], (*ipr)[j] = (*ipr)[j], (*ipr)[i] }

var ufis ipRanges

var nexthops ipRanges

func openIPRanges(fname string, linesToSkip int, sep rune, transform func(string) (int, error)) ipRanges {

	var f io.ReadCloser

	f, err := os.Open(fname)
	defer f.Close()

	if err != nil {
		log.Fatalf("unable to open %s: %s", fname, err)
	}

	if strings.HasSuffix(fname, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			log.Fatalln("error during gzip decompress of: ", fname, ":", err)
		}
		defer gz.Close()
		f = gz
	}

	svr := csv.NewReader(f)
	svr.Comma = sep

	// read and discard header
	for i := 0; i < linesToSkip; i++ {
		svr.Read()
	}

	var ipr ipRanges

	for {
		r, err := svr.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatal(err)
		}

		ipFrom, _ := strconv.Atoi(r[0]) // ignoring errors here
		ipTo, _ := strconv.Atoi(r[1])
		data, _ := transform(r[2])

		ipr = append(ipr, ipRange{rangeFrom: uint32(ipFrom), rangeTo: uint32(ipTo), data: data})
	}

	if !sort.IsSorted(&ipr) {
		sort.Sort(&ipr)
	}

	// log.Println("Loaded", len(ufir), "networks")

	return ipr
}

func lookupRange(ip32 uint32, ipr ipRanges) int {

	idx := sort.Search(ipr.Len(), func(i int) bool { return ip32 <= ipr[i].rangeTo })

	if idx != -1 && ipr[idx].rangeFrom <= ip32 && ip32 <= ipr[idx].rangeTo {
		// log.Printf("Found %04x at offset %d: from=%04x to=%04x\n", ip32, idx, ufis[idx].rangeFrom, ufis[idx].rangeTo)
		return ipr[idx].data
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

	record := gcity.GetRecord(ip)
	var speed, org string
	if gspeed != nil {
		speed, _ /* netmask */ = gspeed.GetName(ip)
	}
	if gisp != nil {
		org = gisp.GetOrg(ip)
	}

	var ufi int
	var nexthop uint32

	if ip4 := netip.To4(); ip4 != nil && (ufis != nil || nexthops != nil) {
		ip32 := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])

		if ufis != nil {
			ufi = lookupRange(ip32, ufis)
		}

		if nexthops != nil {
			nexthop = uint32(lookupRange(ip32, nexthops))
		}
	}

	ipinfo := IPInfo{
		IP:       ip,
		NetSpeed: speed,
		ISP:      org,
		NextHop:  net.IPv4(byte(nexthop>>24), byte(nexthop>>16), byte(nexthop>>8), byte(nexthop)).String(),
	}
	ipinfo.UFI.GuessedUFI = ufi
	// only flesh if we got results
	if r != nil {
		ipinfo.City.City = record.City
		ipinfo.CountryCode = record.CountryCode
		ipinfo.Latitude = record.Latitude
		ipinfo.Longitude = record.Longitude
		ipinfo.Region = record.Region
		ipinfo.RegionName = geoip.GetRegionName(record.CountryCode, record.Region)

		ipinfo.AreaCode = record.AreaCode
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.Encode(ipinfo)
}

func main() {

	dataDir := flag.String("datadir", "", "Directory containing GeoIP data files")
	ufi := flag.String("ufi", "", "File containing iprange-to-UFI mappings")
	nexthop := flag.String("nexthop", "", "File containing next-hop mappings")
	lite := flag.Bool("lite", false, "Load only GeoLiteCity.dat")

	flag.Parse()

	if *lite {
		gcity = opendat(*dataDir, "GeoLiteCity.dat")
	} else {
		gcity = opendat(*dataDir, "GeoIPCity.dat")
		gspeed = opendat(*dataDir, "GeoIPNetSpeed.dat")
		gisp = opendat(*dataDir, "GeoIPISP.dat")
	}

	if *ufi != "" {
		ufis = openIPRanges(*ufi, 1, '\t', strconv.Atoi)
	}

	if *nexthop != "" {
		nexthops = openIPRanges(*nexthop, 0, ',', func(s string) (int, error) {
			netip := net.ParseIP(s)
			if netip == nil {
				return 0, errors.New("bad ip address")
			}

			ip4 := netip.To4()
			ip32 := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
			return int(ip32), nil
		})
	}

	http.HandleFunc("/lookup/", lookupHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
