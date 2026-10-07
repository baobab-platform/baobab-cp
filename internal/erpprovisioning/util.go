package erpprovisioning

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// uniqueStrings is the sorted set of values: erp/v1 declares functional_currencies uniqueItems, and several legal entities
// can share a functional currency.
func uniqueStrings(in []string) []string { return slices.Compact(sorted(in)) }
