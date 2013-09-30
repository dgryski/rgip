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
	"sort"
	"strconv"
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
	UFI          int    `json:"ufi"`
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

type ufiRange struct {
	rangeFrom, rangeTo uint32
	ufi                int
}

type ufiRanges []ufiRange

func (ur *ufiRanges) Len() int           { return len(*ur) }
func (ur *ufiRanges) Less(i, j int) bool { return (*ur)[i].rangeTo < (*ur)[j].rangeTo }
func (ur *ufiRanges) Swap(i, j int)      { (*ur)[i], (*ur)[j] = (*ur)[j], (*ur)[i] }

var ufis ufiRanges

func openufi(fname string) ufiRanges {

	f, err := os.Open(fname)

	if err != nil {
		log.Fatalf("unable to open %s: %s", fname, err)
	}

	tsv := csv.NewReader(f)
	tsv.Comma = '\t'

	// read and discard header
	tsv.Read()

	var ufir ufiRanges

	for {

		r, err := tsv.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatal(err)
		}

		ipFrom, _ := strconv.Atoi(r[0]) // ignoring errors here
		ipTo, _ := strconv.Atoi(r[1])
		ufi, _ := strconv.Atoi(r[2])

		ufir = append(ufir, ufiRange{rangeFrom: uint32(ipFrom), rangeTo: uint32(ipTo), ufi: ufi})
	}

	if !sort.IsSorted(&ufir) {
		sort.Sort(&ufir)
	}

	// log.Println("Loaded", len(ufir), "networks")

	return ufir
}

func lookupHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	qip := r.FormValue("ip")
	ips := strings.Split(qip, ",")

	var m []*IPInfo

	for _, ip := range ips {
		var netip net.IP
		if netip = net.ParseIP(ip); netip == nil {
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

		var ufi int
		if ufis != nil {
			if ip4 := netip.To4(); ip4 != nil {
				ip32 := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])

				// see if we can map this ip into a range with a UFI
				// returns smallest index i such that f() is true
				idx := sort.Search(ufis.Len(), func(i int) bool { return ip32 <= ufis[i].rangeTo })

				if idx != -1 && ufis[idx].rangeFrom <= ip32 && ip32 <= ufis[idx].rangeTo {
					// log.Printf("Found %04x at offset %d: from=%04x to=%04x\n", ip32, idx, ufis[idx].rangeFrom, ufis[idx].rangeTo)
					ufi = ufis[idx].ufi
				}

			}
		}

		ipinfo := IPInfo{IP: ip, Speed: speed, Organization: org, UFI: ufi}
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
	ufi := flag.String("ufi", "", "File containing iprange-to-UFI mappings")
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
		ufis = openufi(*ufi)
	}

	http.HandleFunc("/lookup", lookupHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
