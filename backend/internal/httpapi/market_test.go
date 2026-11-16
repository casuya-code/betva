package httpapi

import "testing"

func intPtr(v int) *int { return &v }

func TestMarketWon(t *testing.T) {
	if !MarketWon(marketFTOver05, 1, 0, nil, nil) {
		t.Fatal("FT over 0.5 should win on 1-0")
	}
	if MarketWon(marketFTOver05, 0, 0, nil, nil) {
		t.Fatal("FT over 0.5 should lose on 0-0")
	}
	if !MarketWon(marketHTOver05, 0, 0, intPtr(1), intPtr(0)) {
		t.Fatal("HT over 0.5 should win on HT 1-0")
	}
	if MarketWon(marketHTOver05, 1, 0, intPtr(0), intPtr(0)) {
		t.Fatal("HT over 0.5 should lose on HT 0-0")
	}
	if MarketWon(marketHTOver05, 1, 0, nil, nil) {
		t.Fatal("HT over 0.5 should not win without HT score")
	}
}
