// Target path: internal/provisioning/manifest_test.go
package provisioning

import "testing"

func TestTenantManifestValidation(t *testing.T) {
	m:=TenantManifest{
		APIVersion:"baobab.nabhold.com/v1",Kind:"TenantProvisioning",
		Metadata:ManifestMetadata{Name:"zuribeans-release-1",TenantID:"tn_zuribeans",DesiredStateVersion:1},
		Spec:TenantManifestSpec{
			LegalEntityID:"le_zuribeans",DigitalEstate:"zuribeans",
			Markets:[]ManifestMarket{{MarketCode:"UG"},{MarketCode:"ZA"}},
			TradeLanes:[]ManifestTradeLane{{OriginMarket:"UG",DestinationMarket:"ZA",Direction:"CROSS_MARKET"}},
		},
	}
	if err:=m.Validate();err!=nil{t.Fatal(err)}
}
