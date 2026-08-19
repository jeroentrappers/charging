package datex

import "testing"

// The Spanish NAP (DGT/MITERD) publishes the same EnergyInfrastructure profile
// as the Belgian feeds, but with three differences this fixture pins down: the
// coordinates sit on coordinatesForDisplay, the address arrives as labelled
// free-text lines under _locationReferenceExtension, and the refill point has no
// externalIdentifier — its EVSE id is the name.
const esFixture = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:d2="http://datex2.eu/schema/3/d2Payload" xmlns:egi="http://datex2.eu/schema/3/energyInfrastructure"
            xmlns:fac="http://datex2.eu/schema/3/facilities" xmlns:com="http://datex2.eu/schema/3/common"
            xmlns:loc="http://datex2.eu/schema/3/locationReferencing" xmlns:locx="http://datex2.eu/schema/3/locationExtension">
  <egi:energyInfrastructureTable>
    <egi:energyInfrastructureSite id="C3AUF6YC73RFPKA5OA3P">
      <fac:name><com:values><com:value lang="es">WS Málaga</com:value></com:values></fac:name>
      <fac:locationReference xsi:type="loc:PointLocation" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
        <loc:_locationReferenceExtension>
          <loc:facilityLocation>
            <locx:address>
              <locx:postcode>29006</locx:postcode>
              <locx:addressLine order="1">
                <locx:type>generalTextLine</locx:type>
                <locx:text><com:values><com:value lang="es">Dirección: Calle Marea Baja, 16</com:value></com:values></locx:text>
              </locx:addressLine>
              <locx:addressLine order="2">
                <locx:type>generalTextLine</locx:type>
                <locx:text><com:values><com:value lang="es">Municipio: Málaga</com:value></com:values></locx:text>
              </locx:addressLine>
            </locx:address>
          </loc:facilityLocation>
        </loc:_locationReferenceExtension>
        <loc:coordinatesForDisplay>
          <loc:latitude>36.711372</loc:latitude>
          <loc:longitude>-4.468101</loc:longitude>
        </loc:coordinatesForDisplay>
      </fac:locationReference>
      <fac:operator><fac:name><com:values><com:value lang="es">Wenea Services Spain, S.L.</com:value></com:values></fac:name></fac:operator>
      <egi:energyInfrastructureStation id="ST1">
        <egi:refillPoint id="ZPLPLQSSNI7MAEDFYK4WBZ3UNMR">
          <fac:name><com:values><com:value lang="es">ES*WEN*EWSMALAGA04DC2</com:value></com:values></fac:name>
          <egi:connector>
            <egi:connectorType>iec62196T2COMBO</egi:connectorType>
            <egi:chargingMode>mode4DC</egi:chargingMode>
            <egi:maxPowerAtSocket>360000.0</egi:maxPowerAtSocket>
          </egi:connector>
        </egi:refillPoint>
      </egi:energyInfrastructureStation>
    </egi:energyInfrastructureSite>
  </egi:energyInfrastructureTable>
</d2:payload>`

func TestParse_SpanishProfile(t *testing.T) {
	conns, _, err := Parse("es-dgt", []byte(esFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 {
		t.Fatalf("want 1 connector, got %d", len(conns))
	}
	c := conns[0]
	// The EVSE id from the name, not the opaque id attribute: the id is not
	// guaranteed stable across the daily republication.
	if c.EVSEUID != "ES*WEN*EWSMALAGA04DC2" {
		t.Errorf("evse uid = %q", c.EVSEUID)
	}
	if c.Lat != 36.711372 || c.Lon != -4.468101 {
		t.Errorf("coords = %v, %v", c.Lat, c.Lon)
	}
	if c.PostalCode != "29006" || c.City != "Málaga" {
		t.Errorf("postcode/city = %q / %q", c.PostalCode, c.City)
	}
	if want := "Calle Marea Baja, 16, 29006 Málaga"; c.Address != want {
		t.Errorf("address = %q, want %q", c.Address, want)
	}
	if want := "Wenea Services Spain, S.L. · WS Málaga"; c.Name != want {
		t.Errorf("name = %q, want %q", c.Name, want)
	}
	if c.PowerKW != 360 || c.CurrentType != "DC" {
		t.Errorf("power/current = %v / %q", c.PowerKW, c.CurrentType)
	}
}

// A refill point that does carry an externalIdentifier must keep using it, so
// the Spanish name fallback cannot change the ids of the existing sources
// (Eco-Movement, Indigo) and churn their history.
func TestRefillPointUID_PrefersExternalIdentifier(t *testing.T) {
	rp := refillPoint{ID: "Indigo-RP-11229", ExternalID: "BEPKGE5300001011", Name: "ES*X*Y"}
	if got := rp.uid(); got != "BEPKGE5300001011" {
		t.Errorf("uid = %q, want the externalIdentifier", got)
	}
	// A plain descriptive name is not an EVSE id, so the id attribute wins.
	plain := refillPoint{ID: "RP-7", Name: "Parking level 2"}
	if got := plain.uid(); got != "RP-7" {
		t.Errorf("uid = %q, want the id attribute", got)
	}
}
