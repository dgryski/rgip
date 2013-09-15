// rgip: restful geoip lookup service
package main

import (
	"encoding/json"
	"flag"
	"github.com/abh/geoip"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
)

type City struct {
	City          string  `json:"city"`
	ContinentCode string  `json:"continent_code"`
	CountryCode   string  `json:"country_code"`
	CountryCode3  string  `json:"country_code3"`
	CountryName   string  `json:"country_name"`
	Latitude      float32 `json:"latitude"`
	Longitude     float32 `json:"longitude"`
	Region        string  `json:"region"`

	AreaCode   int    `json:"area_code"`
	CharSet    int    `json:"char_set"`
	PostalCode string `json:"postal_code"`
}

type IPInfo struct {
	IP           string `json:"ip"`
	City         `json:"city"`
	Organization string `json:"organization"`
	Speed        string `json:"speed"`
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
			ipinfo.City.City = r.City
			ipinfo.ContinentCode = r.ContinentCode
			ipinfo.CountryCode = r.CountryCode
			ipinfo.CountryCode3 = r.CountryCode3
			ipinfo.CountryName = r.CountryName
			ipinfo.Latitude = r.Latitude
			ipinfo.Longitude = r.Longitude
			ipinfo.Region = geoip.GetRegionName(r.CountryCode, r.Region)

			ipinfo.AreaCode = r.AreaCode
			ipinfo.CharSet = r.CharSet
			ipinfo.PostalCode = r.PostalCode

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

	http.HandleFunc("/lookup", lookupHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
