package datex

import "testing"

// Portugal's Mobi.E status publication carries the ad-hoc price as a
// GeneralRateInformation per electricEnergyMixOverride — one override per price
// dimension, using the DATEX II pricingPolicy vocabulary — instead of the
// energyRateUpdate the other AFIR publishers use.
const mobieStatus = `<?xml version="1.0" encoding="UTF-8"?>
<ns4:payload xmlns="http://datex2.eu/schema/3/common" xmlns:ns2="http://datex2.eu/schema/3/facilities"
             xmlns:ns3="http://datex2.eu/schema/3/energyInfrastructure" xmlns:ns4="http://datex2.eu/schema/3/d2Payload"
             xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="ns3:EnergyInfrastructureStatusPublication">
  <publicationCreator><country>pt</country><nationalIdentifier>MOBIE</nationalIdentifier></publicationCreator>
  <ns3:energyInfrastructureSiteStatus>
    <ns2:reference targetClass="fac:FacilityObject" id="EZC-AMD-00051"/>
    <ns3:energyInfrastructureStationStatus>
      <ns2:reference targetClass="fac:FacilityObject" id="EZC-AMD-00051"/>
      <ns3:refillPointStatus xsi:type="ns3:ElectricChargingPointStatus">
        <ns2:reference targetClass="fac:FacilityObject" id="AMD-00051-1"/>
        <ns3:status>available</ns3:status>
        <ns3:electricEnergyMixOverride energyMixIndex="1">
          <ns3:rates xsi:type="ns2:GeneralRateInformation">
            <ns2:energyPricingPolicy>
              <ns3:pricingPolicy>pricePerDeliveryUnit</ns3:pricingPolicy>
              <ns3:minimumDeliveryFee>0.26</ns3:minimumDeliveryFee>
            </ns2:energyPricingPolicy>
            <ns2:applicableCurrency>EUR</ns2:applicableCurrency>
          </ns3:rates>
        </ns3:electricEnergyMixOverride>
        <ns3:electricEnergyMixOverride energyMixIndex="2">
          <ns3:rates xsi:type="ns2:GeneralRateInformation">
            <ns2:energyPricingPolicy>
              <ns3:pricingPolicy>pricePerChargingTime</ns3:pricingPolicy>
              <ns3:minimumDeliveryFee>0.03</ns3:minimumDeliveryFee>
            </ns2:energyPricingPolicy>
            <ns2:applicableCurrency>EUR</ns2:applicableCurrency>
          </ns3:rates>
        </ns3:electricEnergyMixOverride>
        <ns3:electricEnergyMixOverride energyMixIndex="3">
          <ns3:rates xsi:type="ns2:GeneralRateInformation">
            <ns2:energyPricingPolicy>
              <ns3:pricingPolicy>flatRate</ns3:pricingPolicy>
              <ns3:minimumDeliveryFee>0.16</ns3:minimumDeliveryFee>
            </ns2:energyPricingPolicy>
            <ns2:applicableCurrency>EUR</ns2:applicableCurrency>
          </ns3:rates>
        </ns3:electricEnergyMixOverride>
      </ns3:refillPointStatus>
      <ns3:refillPointStatus xsi:type="ns3:ElectricChargingPointStatus">
        <ns2:reference targetClass="fac:FacilityObject" id="AMD-00051-2"/>
        <ns3:status>occupied</ns3:status>
        <ns3:electricEnergyMixOverride energyMixIndex="1">
          <ns3:rates xsi:type="ns2:GeneralRateInformation">
            <ns2:energyPricingPolicy>
              <ns3:pricingPolicy>pricePerDeliveryUnit</ns3:pricingPolicy>
              <ns3:minimumDeliveryFee>0.00</ns3:minimumDeliveryFee>
            </ns2:energyPricingPolicy>
            <ns2:applicableCurrency>EUR</ns2:applicableCurrency>
          </ns3:rates>
        </ns3:electricEnergyMixOverride>
      </ns3:refillPointStatus>
    </ns3:energyInfrastructureStationStatus>
  </ns3:energyInfrastructureSiteStatus>
</ns4:payload>`

func TestParseAFIRStatus_MobiEPricingPolicy(t *testing.T) {
	st, err := ParseAFIRStatus([]byte(mobieStatus))
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 2 {
		t.Fatalf("want 2 refill points, got %d", len(st))
	}

	one := st["AMD-00051-1"]
	if one.Status != "AVAILABLE" {
		t.Errorf("status = %q", one.Status)
	}
	if one.Tariff == nil {
		t.Fatal("want a tariff from the pricingPolicy rates")
	}
	if one.Tariff.Currency != "EUR" {
		t.Errorf("currency = %q", one.Tariff.Currency)
	}
	got := map[string]float64{}
	for _, el := range one.Tariff.Elements {
		for _, pc := range el.PriceComponents {
			got[pc.Type] = pc.Price
		}
	}
	// pricePerDeliveryUnit is €/kWh, flatRate is €/session, and
	// pricePerChargingTime is €/minute converted to our per-hour TIME component.
	want := map[string]float64{"ENERGY": 0.26, "TIME": 1.8, "FLAT": 0.16}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v (got %v)", k, got[k], v, got)
		}
	}

	// A point whose only published dimension is 0.00 must stay unpriced rather
	// than become the cheapest charger on the map.
	two := st["AMD-00051-2"]
	if two.Status != "CHARGING" {
		t.Errorf("status = %q", two.Status)
	}
	if two.Tariff != nil {
		t.Errorf("want no tariff for an all-zero rate, got %+v", two.Tariff)
	}
}
