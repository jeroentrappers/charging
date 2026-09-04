package ecomovement

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/appmire/charging/internal/model"
)

// One page of the Belgian NAP feed: the table publication and the status
// publication for the same sites, at the document root (no envelope). %s is the
// site/point discriminator so the test server can hand out two distinct pages.
const pageTmpl = `{"modelBaseVersionG":"3","profileNameG":"AFIR Energy Infrastructure","profileVersionG":"01-00-00",
 "aegiEnergyInfrastructureTablePublication":{
   "lang":"en","publicationTime":"2026-09-03T22:43:57Z",
   "publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
   "energyInfrastructureTable":[{"idG":"table-1","versionG":"1",
     "energyInfrastructureSite":[
       {"idG":"site-%[1]s","versionG":"1",
        "entrance":[{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.78419,"longitude":3.35766},
          "locLocationExtensionG":{"FacilityLocation":{"address":{"postcode":"8554",
            "city":{"values":[{"lang":"be","value":"SINT-DENIJS"}]},"countryCode":"BE",
            "addressLine":[{"order":0,"type":{"value":"street"},"text":{"values":[{"lang":"be","value":"HOOGSTRAAT 24 C"}]}}]}}}}}],
        "energyDistributor":{"afacAnOrganisation":{"name":{"values":[{"lang":"be","value":"Eneco"}]}}},
        "energyInfrastructureStation":[{"idG":"station-%[1]s","versionG":"1","totalMaximumPower":22000,"numberOfRefillPoints":1,
          "refillPoint":[{"aegiElectricChargingPoint":{"idG":"BE-ENE-POINT_%[1]s-1","versionG":"1",
            "deliveryUnit":{"value":"kWh"},"currentType":{"value":"ac"},"numberOfConnectors":1,
            "connector":[{"externalIdentifier":[{"identifier":"BE*ENE*EPOINT%[1]s*1",
                "typeOfIdentifier":{"value":"extendedG","extendedValueG":"evseId"}}],
              "connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":22000,"voltage":230,"maximumCurrent":32}]}}]}]}
     ]}]},
 "aegiEnergyInfrastructureStatusPublication":{
   "lang":"en","publicationTime":"2026-09-03T22:43:57Z",
   "publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
   "energyInfrastructureSiteStatus":[
     {"reference":{"idG":"site-%[1]s"},
      "energyInfrastructureStationStatus":[{"reference":{"idG":"station-%[1]s"},
        "refillPointStatus":[{"aegiElectricChargingPointStatus":{
          "reference":{"idG":"BE-ENE-POINT_%[1]s-1"},"status":{"value":"available"},
          "energyRateUpdate":[{"energyRateReference":{"idG":"rate-%[1]s"},
            "energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.3722,"taxIncluded":false,"taxRate":21}]}]}}]}]}
   ]}}`

// emptyPage is what the feed serves past the last offset: a table with no sites.
const emptyPage = `{"modelBaseVersionG":"3","aegiEnergyInfrastructureTablePublication":{
  "lang":"en","publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
  "energyInfrastructureTable":[{"idG":"table-1","versionG":"1"}]}}`

// napServer serves `pages` pages of one site each, checking auth and paging.
func napServer(t *testing.T, pages int, sendLink bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		seen = append(seen, q.Get("offset"))
		if q.Get("limit") != strconv.Itoa(pageSize) {
			t.Errorf("limit = %q, want %d", q.Get("limit"), pageSize)
		}
		off, _ := strconv.Atoi(q.Get("offset"))
		page := off / pageSize
		w.Header().Set("Content-Type", "application/json")
		if page >= pages {
			_, _ = w.Write([]byte(emptyPage))
			return
		}
		if sendLink && page+1 < pages {
			next := *r.URL
			next.Scheme, next.Host = "http", r.Host
			nq := next.Query()
			nq.Set("offset", strconv.Itoa((page+1)*pageSize))
			next.RawQuery = nq.Encode()
			w.Header().Set("Link", `<`+next.String()+`>; rel="next"`)
		}
		_, _ = w.Write([]byte(pageBody(page)))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func pageBody(page int) string {
	return fmt.Sprintf(pageTmpl, strconv.Itoa(page))
}

func TestFetch_WalksPagesAndJoinsStatusAndPrice(t *testing.T) {
	srv, seen := napServer(t, 3, false)

	conns, tariffs, err := Fetch(context.Background(), "ecomovement", srv.URL+"/datex2/v1/locations", "tok")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := len(conns), 3; got != want {
		t.Fatalf("connectors = %d, want %d (one per page)", got, want)
	}
	if got, want := len(*seen), 4; got != want { // 3 pages + the empty one that stops the walk
		t.Errorf("requests = %d (%v), want %d", got, *seen, want)
	}

	c := conns[0]
	if c.CPOID != "ecomovement" {
		t.Errorf("CPOID = %q", c.CPOID)
	}
	// Chargers are keyed by the roaming EVSE id, not the publisher's idG.
	if want := "BE*ENE*EPOINT0*1"; c.EVSEUID != want {
		t.Errorf("EVSEUID = %q, want %q", c.EVSEUID, want)
	}
	if c.EVSEStatus != "AVAILABLE" {
		t.Errorf("status = %q, want AVAILABLE (status publication not joined?)", c.EVSEStatus)
	}
	if c.PowerKW != 22 || c.PlugType != model.NormalizePlug("iec62196T2") {
		t.Errorf("power/plug = %v/%q", c.PowerKW, c.PlugType)
	}
	if c.TariffID != "rate-0" {
		t.Errorf("TariffID = %q, want rate-0", c.TariffID)
	}
	tar, ok := tariffs["rate-0"]
	if !ok {
		t.Fatalf("tariff rate-0 missing; have %v", tariffs)
	}
	if got, want := tar.Elements[0].PriceComponents[0].Price, 0.4504; got != want {
		t.Errorf("ENERGY = %v, want %v (net 0.3722 + 21%% VAT)", got, want)
	}
	if got, want := len(tariffs), 3; got != want {
		t.Errorf("tariffs = %d, want %d", got, want)
	}
}

func TestFetch_FollowsLinkHeader(t *testing.T) {
	srv, seen := napServer(t, 2, true)

	conns, _, err := Fetch(context.Background(), "ecomovement", srv.URL+"/datex2/v1/locations", "tok")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := len(conns), 2; got != want {
		t.Fatalf("connectors = %d, want %d", got, want)
	}
	if got, want := (*seen)[1], strconv.Itoa(pageSize); got != want {
		t.Errorf("second request offset = %q, want %q", got, want)
	}
}

func TestFetch_ErrorsOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if _, _, err := Fetch(context.Background(), "ecomovement", srv.URL, ""); err == nil {
		t.Fatal("want an error on HTTP 403")
	}
}

func TestNextLink(t *testing.T) {
	cases := map[string]string{
		`<https://x/y?offset=1000>; rel="next"`:                       "https://x/y?offset=1000",
		`<https://x/y?offset=0>; rel="prev", <https://x/z>; rel=next`: "https://x/z",
		`<https://x/y>; rel="last"`:                                   "",
		``:                                                            "",
	}
	for header, want := range cases {
		if got := nextLink(header); got != want {
			t.Errorf("nextLink(%q) = %q, want %q", header, got, want)
		}
	}
}

// The feed publishes some physical EVSEs twice — a live record and a
// decommissioned one, each with its own tariff (Lidl's Belgian sites). Both map
// to the same charger, so the live one must win rather than whichever copy the
// walk read last.
func TestCollapseDuplicates_PrefersTheLiveRecord(t *testing.T) {
	ghost := model.Connector{EVSEUID: "BE*LDL*E00000011", ConnectorID: "1", PowerKW: 50, EVSEStatus: "OUTOFORDER", TariffID: "old", PlugType: "CHADEMO"}
	live := model.Connector{EVSEUID: "BE*LDL*E00000011", ConnectorID: "1", PowerKW: 50, EVSEStatus: "AVAILABLE", TariffID: "current", PlugType: "CHADEMO"}
	other := model.Connector{EVSEUID: "BE*LDL*E00000010", ConnectorID: "1", PowerKW: 50, EVSEStatus: "AVAILABLE", TariffID: "current"}

	for _, tc := range []struct {
		name string
		in   []model.Connector
	}{
		{"ghost first", []model.Connector{ghost, live, other}},
		{"live first", []model.Connector{live, ghost, other}},
	} {
		got := collapseDuplicates(tc.in)
		if len(got) != 2 {
			t.Fatalf("%s: %d connectors, want 2", tc.name, len(got))
		}
		if got[0].EVSEUID != "BE*LDL*E00000011" {
			t.Errorf("%s: order not stable: %v", tc.name, got[0].EVSEUID)
		}
		if got[0].EVSEStatus != "AVAILABLE" || got[0].TariffID != "current" {
			t.Errorf("%s: kept %s/%s, want AVAILABLE/current", tc.name, got[0].EVSEStatus, got[0].TariffID)
		}
	}

	// Two dead copies: the priced one is still the better record.
	unpriced := ghost
	unpriced.TariffID = ""
	got := collapseDuplicates([]model.Connector{unpriced, ghost})
	if len(got) != 1 || got[0].TariffID != "old" {
		t.Errorf("dead copies: kept %+v, want the priced one", got)
	}

	// Distinct connectors of one EVSE are not duplicates.
	c2 := live
	c2.ConnectorID = "2"
	if got := collapseDuplicates([]model.Connector{live, c2}); len(got) != 2 {
		t.Errorf("connector 1 and 2 of one EVSE collapsed: %d rows", len(got))
	}
}
