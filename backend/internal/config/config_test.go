package config

import "testing"

func TestParseStakeTiers(t *testing.T) {
	got := parseStakeTiers("50000,100000, 200000,500000")
	want := []int64{50000, 100000, 200000, 500000}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%d want %d", i, got[i], want[i])
		}
	}

	// empty/invalid input falls back to defaults
	def := parseStakeTiers("")
	if len(def) == 0 || def[0] != 50000 {
		t.Fatalf("default tiers = %v", def)
	}
}

func TestIsValidStakeAndPayout(t *testing.T) {
	c := &Config{
		StakeTiers:       []int64{50000, 100000},
		PayoutMultiplier: 15000,
	}
	if !c.IsValidStake(50000) || c.IsValidStake(123) {
		t.Fatal("IsValidStake mismatch")
	}
	if got := c.PayoutFor(100000); got != 150000 {
		t.Fatalf("PayoutFor=%d want 150000", got)
	}
}

func TestAtoi(t *testing.T) {
	if got := Atoi("42", 1); got != 42 {
		t.Fatalf("got %d", got)
	}
	if got := Atoi("nope", 7); got != 7 {
		t.Fatalf("default got %d", got)
	}
}
