package development

import (
	"github.com/baobab-platform/baobab-cp/internal/billing"
	"testing"
)

func TestClosedCMSCapabilityRegistration(t *testing.T) {
	raw, err := cmsCapabilityProfile.registration()
	if err != nil {
		t.Fatal(err)
	}
	registration, err := billing.ParseRegistration(raw)
	if err != nil {
		t.Fatal(err)
	}
	if registration.Repository != "baobab-cms" || registration.Provider.ProviderKey != "baobab-cms.payload" || registration.Provider.Lifecycle != "DRAFT" || registration.Provider.ProductionPermitted || registration.Provider.Simulated {
		t.Fatal("CMS fixture invented provider permission")
	}
	if len(registration.Capabilities) != 1 || registration.Capabilities[0].Key != "content.entry.resolve" || len(registration.Support) != 1 {
		t.Fatal("CMS fixture expanded capability authority")
	}
	if registration.Provider.Invocation == nil || registration.Provider.Invocation.ServiceReference != "service://baobab-cms/content" {
		t.Fatal("CMS invocation differs from owner declaration")
	}
	if len(cmsCapabilityProfile.support()) != 1 || len(pulseCapabilityProfile.support()) != 2 || cmsCapabilityProfile.product == pulseCapabilityProfile.product {
		t.Fatal("native profile authority crossed engine boundary")
	}
}
