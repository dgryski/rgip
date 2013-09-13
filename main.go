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
	mapping map[int32]string
	m       sync.Mutex
}

func s2int(s1, s2 string) int32 {
	return int32(s1[0])<<24 | int32(s1[1])<<16 | int32(s2[0])<<8 | int32(s2[1])
}

func NewRegionMapping(regioncsv string) *regionMapping {

	rmap := &regionMapping{mapping: make(map[int32]string)}

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

		rmap.mapping[s2int(r[0], r[1])] = r[2]
	}

	return rmap
}

func (rc *regionMapping) lookupRegion(cc, rcode string) string {
	rc.m.Lock()
	defer rc.m.Unlock()
	return rc.mapping[s2int(cc, rcode)]
}

func lookupHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	qip := r.FormValue("ip")
	ips := strings.Split(qip, ",")

	var m []*IPInfo

	for _, ip := range ips {
		if netip := net.ParseIP(ip); netip == nil {
			// failed
			m = append(m, nil)
			continue
		}

		r := gcity.GetRecord(ip)
		var speed, org string
		if gspeed != nil {
			speed, _ /* netmask */ = gspeed.GetName(ip)
		}
		if gisp != nil {
			org = gisp.GetOrg(ip)
		}

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
	lite := flag.Bool("lite", false, "Load only GeoLiteCity.dat")

	flag.Parse()

	if *lite {
		gcity = opendat(*dataDir, "GeoLiteCity.dat")
	} else {
		gcity = opendat(*dataDir, "GeoIPCity.dat")
		gspeed = opendat(*dataDir, "GeoIPNetSpeed.dat")
		gisp = opendat(*dataDir, "GeoIPISP.dat")

	}
	rmap = NewRegionMapping(path.Join(*dataDir, "region.csv"))

	http.HandleFunc("/lookup", lookupHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
