package domain

import "testing"

func TestEngineIdentifiersFollowTheSharedGrammar(t *testing.T) {
	for id, valid := range map[string]bool{"baobab-trade": true, "baobab-payments": true, "baobab_trade": false, "Baobab-Trade": false, "trade-": false, "ab": false} {
		if ValidEngineID(id) != valid {
			t.Errorf("ValidEngineID(%q) = %v", id, !valid)
		}
	}
	for id, valid := range map[string]bool{"ei_0199a1b2c3d4": true, "ei_medusaprodza01": true, "ei_": false, "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b": false, "trade_eu_1": false, "EI_abc123": false} {
		if ValidEngineInstanceID(id) != valid {
			t.Errorf("ValidEngineInstanceID(%q) = %v", id, !valid)
		}
	}
}

// TestEngineInstanceKeyMatchesTheGeneratedColumn: the key is what migration
// 000058 generates from the UUID ('ei_' || replace(uuid, '-', ”)), and a
// canonical identifier is left as it is.
func TestEngineInstanceKeyMatchesTheGeneratedColumn(t *testing.T) {
	cases := map[string]string{
		"0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b": "ei_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b",
		"0199A1B2-C3D4-7E8F-9A0B-1C2D3E4F5A6B": "ei_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b",
		"ei_medusaprodza01":                    "ei_medusaprodza01",
	}
	for id, want := range cases {
		if got := EngineInstanceKey(id); got != want || !ValidEngineInstanceID(got) {
			t.Errorf("EngineInstanceKey(%q) = %q, want %q", id, got, want)
		}
	}
}
