package repository

import (
	"strings"
	"testing"
)

// LA-07: a readback projection must never expand the recorded mandate into a
// false all-markets/all-activities permission or leak arbitrarily large data.
func TestBoundedMandateScope(t *testing.T) {
	cases := []struct {
		name     string
		values   []string
		minCount int
		minLen   int
		maxLen   int
		want     bool
	}{
		{"allowed activity", []string{"wholesale-order"}, 1, 3, 160, true},
		{"missing mandatory activity", nil, 1, 3, 160, false},
		{"duplicate activity", []string{"trade.export", "trade.export"}, 1, 3, 160, false},
		{"short activity", []string{"x"}, 1, 3, 160, false},
		{"contract-valid spaced activity", []string{" trade.export "}, 1, 3, 160, true},
		{"contract-valid unicode activity", []string{strings.Repeat("界", 160)}, 1, 3, 160, true},
		{"too many unicode characters", []string{strings.Repeat("界", 161)}, 1, 3, 160, false},
		{"empty optional capability filter", []string{}, 0, 3, 120, true},
		{"capability too long", []string{string(make([]byte, 121))}, 0, 3, 120, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := boundedMandateScope(tt.values, tt.minCount, tt.minLen, tt.maxLen); got != tt.want {
				t.Fatalf("bounded scope = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestValidMandateMarketScope(t *testing.T) {
	for _, tt := range []struct {
		name    string
		markets []string
		want    bool
	}{
		{"two countries", []string{"ZA", "UG"}, true},
		{"no market", nil, false},
		{"lowercase", []string{"za"}, false},
		{"duplicate", []string{"ZA", "ZA"}, false},
		{"long region", []string{"af-south-1"}, false},
		{"empty", []string{""}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := validMandateMarketScope(tt.markets); got != tt.want {
				t.Fatalf("market scope = %t, want %t", got, tt.want)
			}
		})
	}
}
