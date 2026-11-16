package httpapi

import (
	"testing"
	"time"
)

// RFC 6238 test secret: ASCII "12345678901234567890".
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestVerifyTOTPAt(t *testing.T) {
	if !verifyTOTPAt(rfcSecret, "287082", time.Unix(59, 0)) {
		t.Fatal("RFC 6238 vector at t=59 should verify")
	}
	if verifyTOTPAt(rfcSecret, "000000", time.Unix(59, 0)) {
		t.Fatal("wrong code must be rejected")
	}
	if !verifyTOTPAt(rfcSecret, "287082", time.Unix(89, 0)) {
		t.Fatal("one step of clock drift should be accepted")
	}
	if verifyTOTPAt(rfcSecret, "287082", time.Unix(120, 0)) {
		t.Fatal("a code two steps old must be rejected")
	}
	if verifyTOTPAt(rfcSecret, "28708", time.Unix(59, 0)) {
		t.Fatal("short code must be rejected")
	}
}

func TestVerifyAdminSecondFactor(t *testing.T) {
	if !verifyAdminSecondFactor("", "", "") {
		t.Fatal("without a configured secret, 2FA is disabled")
	}
	code, err := CurrentTOTP(rfcSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyAdminSecondFactor(rfcSecret, "recovery-abc", code) {
		t.Fatal("current TOTP code should be accepted")
	}
	if !verifyAdminSecondFactor(rfcSecret, "recovery-abc", "recovery-abc") {
		t.Fatal("recovery code should be accepted")
	}
	if verifyAdminSecondFactor(rfcSecret, "recovery-abc", "000000") {
		t.Fatal("bad 2FA code must be rejected")
	}
	if verifyAdminSecondFactor(rfcSecret, "", "") {
		t.Fatal("missing 2FA code must be rejected")
	}
}
