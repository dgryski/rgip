// rgip: restful geoip lookup service
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"github.com/abh/geoip"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
)

type IPInfo struct {
	IP           string
	CountryCode  string
	CountryName  string
	City         string
	Region       string
	Latitude     float32
	Longitude    float32
	Organization string
	Speed        string
}

var gcity, gspeed, gisp *geoip.GeoIP
var rmap *regionMapping

func opendat(dataDir string, dat string) *geoip.GeoIP {
	fname := path.Join(dataDir, dat)
	g, err := geoip.Open(fname)
	if err != nil {
		log.Fatalf("unable to open %s: %s", fname, err)
	}

	return g
}

type regionMapping struct {
	mapping map[string]string
	m       sync.Mutex
}

func NewRegionMapping(regioncsv string) *regionMapping {

	rmap := &regionMapping{mapping: make(map[string]string)}

	f, err := os.Open(regioncsv)
	if err != nil {
		log.Fatalf("unable to open %s: %s", regioncsv, err)
	}
	defer f.Close()
	csvr := csv.NewReader(f)

	for {
		r, err := csvr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal("error during csv read:", err)
		}

		rmap.mapping[r[0]+r[1]] = r[2]
	}

	return rmap
}

func (rc *regionMapping) lookupRegion(cc, rcode string) string {
	rc.m.Lock()
	defer rc.m.Unlock()
	return rc.mapping[cc+rcode]
}

func lookupHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	qip := r.FormValue("ip")
	ips := strings.Split(qip, ",")

	var m []*IPInfo

	for _, ip := range ips {
		// try parsing the IP first
		if netip := net.ParseIP(ip); netip == nil {
			// failed
			m = append(m, nil)
			continue
		}

		r := gcity.GetRecord(ip)
		speed, _ /* netmask */ := gspeed.GetName(ip)
		org := gisp.GetOrg(ip)

		// strings, we always have these so use them in the constructor
		ipinfo := IPInfo{IP: ip, Speed: speed, Organization: org}
		// only flesh if we got results
		if r != nil {
			ipinfo.CountryCode = r.CountryCode
			ipinfo.CountryName = r.CountryName
			ipinfo.Region = rmap.lookupRegion(r.CountryCode, r.Region)
			ipinfo.City = r.City
			ipinfo.Latitude = r.Latitude
			ipinfo.Longitude = r.Longitude
		}
		m = append(m, &ipinfo)
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	switch len(m) {
	case 0:
		w.Write([]byte("{}"))
	case 1:
		encoder.Encode(m[0])
	default:
		encoder.Encode(m)

	}
}

func main() {

	dataDir := flag.String("datadir", "", "Directory containing GeoIP data files")

	flag.Parse()

	gcity = opendat(*dataDir, "GeoIPCity.dat")
	gspeed = opendat(*dataDir, "GeoIPNetSpeed.dat")
	gisp = opendat(*dataDir, "GeoIPISP.dat")
	rmap = NewRegionMapping(path.Join(*dataDir, "region.csv"))

	http.HandleFunc("/lookup", lookupHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
