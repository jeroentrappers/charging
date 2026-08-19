package oicp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Shaped like the live SFOE files, including the three encoding quirks they
// contain: `power` as a string, the postcode as a number, and
// ChargingStationNames as a bare object instead of a list.
const dataJSON = `{"EVSEData":[
 {"OperatorID":"CH*CCC","OperatorName":"Move","EVSEDataRecord":[
   {"EvseID":"CH*CCI*E22078","Accessibility":"Paying publicly accessible",
    "Address":{"Street":"Esplanade des Particules 1","City":"Meyrin","PostalCode":"1217"},
    "ChargingFacilities":[{"Amperage":32,"Voltage":230,"power":"22.0","powertype":"AC_3_PHASE"}],
    "ChargingStationNames":[{"lang":"en","value":"SIG CERN"}],
    "GeoCoordinates":{"Google":"46.23432 6.055602"},
    "Plugs":["Type 2 Outlet"]},
   {"EvseID":"CH*CCI*E30001","Accessibility":"Free publicly accessible",
    "Address":{"Street":"Bahnhofstrasse 3","City":"Zug","PostalCode":6300},
    "ChargingFacilities":[{"Amperage":32,"Voltage":400,"power":0,"powertype":"AC_3_PHASE"},
                          {"Amperage":300,"Voltage":500,"power":150,"powertype":"DC"}],
    "ChargingStationNames":{"lang":"de","value":"Zug Bahnhof"},
    "GeoCoordinates":{"Google":"47.17242 8.51726"},
    "Plugs":["Type 2 Outlet","CCS Combo 2 Plug (Cable Attached)"]},
   {"EvseID":"CH*PRIV*E1","Accessibility":"Restricted access",
    "Address":{"Street":"Werkhof 1","City":"Bern","PostalCode":"3000"},
    "ChargingFacilities":[{"power":11,"powertype":"AC_3_PHASE"}],
    "ChargingStationNames":null,
    "GeoCoordinates":{"Google":"46.94809 7.44744"},
    "Plugs":["Type 2 Outlet"]},
   {"EvseID":"CH*NOGEO*E1","Accessibility":"Paying publicly accessible",
    "Address":{"Street":"","City":"","PostalCode":""},
    "ChargingFacilities":[{"power":11,"powertype":"AC_3_PHASE"}],
    "ChargingStationNames":[],
    "GeoCoordinates":{"Google":""},
    "Plugs":["Type 2 Outlet"]}
 ]}]}`

const statusJSON = `{"EVSEStatuses":[{"EVSEStatusRecord":[
  {"EvseID":"CH*CCI*E22078","EVSEStatus":"Available"},
  {"EvseID":"CH*CCI*E30001","EVSEStatus":"Occupied"},
  {"EvseID":"CH*PRIV*E1","EVSEStatus":"OutOfService"}
]}]}`

func TestParseData(t *testing.T) {
	conns, err := ParseData("ch-sfoe", []byte(dataJSON))
	if err != nil {
		t.Fatal(err)
	}
	// The restricted-access point is not public, and the coordinate-less record
	// can't be placed on a map — both are dropped.
	if len(conns) != 2 {
		t.Fatalf("want 2 connectors, got %d: %+v", len(conns), conns)
	}

	by := map[string]int{}
	for i, c := range conns {
		by[c.EVSEUID] = i
	}
	first := conns[by["CH*CCI*E22078"]]
	if first.PowerKW != 22 { // `power` arrives as the string "22.0"
		t.Errorf("power = %v, want 22", first.PowerKW)
	}
	if first.PlugType != "IEC_62196_T2" || first.CurrentType != "AC" {
		t.Errorf("plug/current = %q / %q", first.PlugType, first.CurrentType)
	}
	if first.Name != "Move · SIG CERN" {
		t.Errorf("name = %q", first.Name)
	}
	if want := "Esplanade des Particules 1, 1217 Meyrin"; first.Address != want {
		t.Errorf("address = %q, want %q", first.Address, want)
	}

	second := conns[by["CH*CCI*E30001"]]
	if second.PowerKW != 150 { // strongest of the two facilities
		t.Errorf("power = %v, want 150", second.PowerKW)
	}
	if second.PlugType != "IEC_62196_T2_COMBO" || second.CurrentType != "DC" {
		t.Errorf("plug/current = %q / %q", second.PlugType, second.CurrentType)
	}
	if second.PostalCode != "6300" { // published as a number
		t.Errorf("postcode = %q", second.PostalCode)
	}
	if second.Name != "Move · Zug Bahnhof" { // names arrived as a bare object
		t.Errorf("name = %q", second.Name)
	}
	// "Free publicly accessible" means freely accessible, NOT free of charge, so
	// it must never become a price.
	if second.TariffID != "" {
		t.Errorf("tariff id = %q, want none", second.TariffID)
	}
}

func TestParseStatus(t *testing.T) {
	st, err := ParseStatus([]byte(statusJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"CH*CCI*E22078": "AVAILABLE",
		"CH*CCI*E30001": "CHARGING",
		"CH*PRIV*E1":    "OUTOFORDER",
	}
	for id, w := range want {
		if st[id] != w {
			t.Errorf("%s = %q, want %q", id, st[id], w)
		}
	}
}

func TestFetch_OverlaysStatusAndCarriesNoTariffs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data":
			_, _ = w.Write([]byte(dataJSON))
		case "/status":
			_, _ = w.Write([]byte(statusJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conns, tariffs, err := Fetch(context.Background(), "ch-sfoe", srv.URL+"/data|"+srv.URL+"/status", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tariffs) != 0 {
		t.Errorf("OICP carries no price; got %d tariffs", len(tariffs))
	}
	got := map[string]string{}
	for _, c := range conns {
		got[c.EVSEUID] = c.EVSEStatus
	}
	if got["CH*CCI*E22078"] != "AVAILABLE" || got["CH*CCI*E30001"] != "CHARGING" {
		t.Errorf("statuses = %v", got)
	}
}
