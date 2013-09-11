// rgip: restful geoip lookup service
package main

import (
	"encoding/json"
	"github.com/abh/geoip"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {

	geo, err := geoip.Open("GeoLiteCity.dat")
	if err != nil {
		log.Fatal("can't open data file: ", err)
	}

	http.HandleFunc("/lookup", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		qip := r.FormValue("ip")
		ips := strings.Split(qip, ",")

		var m []*geoip.GeoIPRecord

		for _, ip := range ips {
			r := geo.GetRecord(ip)
			m = append(m, r)
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
	})

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
