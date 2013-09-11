// rgip: restful geoip lookup service
package main

import (
	"encoding/json"
	"github.com/abh/geoip"
	"log"
	"net/http"
	"os"
)

func main() {

	geo, err := geoip.Open("GeoLiteCity.dat")
	if err != nil {
		log.Fatal("can't open data file: ", err)
	}

	http.HandleFunc("/lookup", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		qip := r.FormValue("ip")
		m := geo.GetRecord(qip)
		s, _ := json.Marshal(m)
		w.Header().Set("Content-Type", "application/json")
		w.Write(s)
	})

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Fatal(http.ListenAndServe(port, nil))
}
