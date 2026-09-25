package subscription

import (
	"errors"
	"fmt"
	"sync"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"gopkg.in/yaml.v3"
)

// billing-policy.yaml as Shared publishes it (embedded at the pinned commit).
const billingPolicyPath = "product/v1/billing-policy.yaml"

var (
	policiesOnce sync.Once
	policies     map[domain.SubscriptionType]domain.BillingPolicy
	policiesErr  error
)

// BillingPolicies returns the billing policy of every subscription type,
// each checked against the contract's billingPolicy.
func BillingPolicies() (map[domain.SubscriptionType]domain.BillingPolicy, error) {
	policiesOnce.Do(func() {
		policies, policiesErr = loadBillingPolicies()
	})
	return policies, policiesErr
}

func loadBillingPolicies() (map[domain.SubscriptionType]domain.BillingPolicy, error) {
	raw, err := contracts.ReadEmbedded(billingPolicyPath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Version  int                                              `yaml:"version"`
		Policies map[domain.SubscriptionType]domain.BillingPolicy `yaml:"policies"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", billingPolicyPath, err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d", billingPolicyPath, doc.Version)
	}
	schema, err := contracts.Compile("product/v1/subscription.schema.json#/$defs/billingPolicy")
	if err != nil {
		return nil, err
	}
	for _, t := range []domain.SubscriptionType{domain.SubscriptionCommercial, domain.SubscriptionInternal, domain.SubscriptionTrial,
		domain.SubscriptionPartner, domain.SubscriptionManual, domain.SubscriptionMigration} {
		p, ok := doc.Policies[t]
		if !ok {
			return nil, fmt.Errorf("%s: no policy for %s", billingPolicyPath, t)
		}
		var verr *contracts.ValidationError
		if err := contracts.ValidateValue(schema, p); errors.As(err, &verr) {
			return nil, fmt.Errorf("%s: %s: %v", billingPolicyPath, t, verr.Problems)
		} else if err != nil {
			return nil, err
		}
	}
	return doc.Policies, nil
}

// PolicyFor returns the billing policy of t.
func PolicyFor(t domain.SubscriptionType) (domain.BillingPolicy, error) {
	all, err := BillingPolicies()
	if err != nil {
		return domain.BillingPolicy{}, err
	}
	p, ok := all[t]
	if !ok {
		return domain.BillingPolicy{}, fmt.Errorf("no billing policy for subscription type %q", t)
	}
	return p, nil
}
