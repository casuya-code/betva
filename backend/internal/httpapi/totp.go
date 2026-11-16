package httpapi

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// TOTP follows RFC 6238: HMAC-SHA1, 30-second steps, six digits, with one
// step of clock drift allowed in either direction. The admin secret lives in
// the environment alongside the admin password, not in the database.
func verifyTOTPAt(secret, code string, at time.Time) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}

	key, err := decodeTOTPSecret(secret)
	if err != nil || len(key) == 0 {
		return false
	}

	counter := uint64(at.Unix() / 30)
	for delta := int64(-1); delta <= 1; delta++ {
		c := counter
		if delta < 0 {
			c -= uint64(-delta)
		} else {
			c += uint64(delta)
		}
		want, err := totpCode(key, c)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(secret, " ", "")))
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
}

func totpCode(key []byte, counter uint64) (string, error) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	if _, err := mac.Write(buf); err != nil {
		return "", err
	}
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

// CurrentTOTP returns the code for the current step. Used by cmd/totp for
// enrollment and by tests.
func CurrentTOTP(secret string) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return totpCode(key, uint64(time.Now().Unix()/30))
}

// verifyAdminSecondFactor accepts a current TOTP code or the configured
// recovery code. With no secret configured, 2FA is disabled.
func verifyAdminSecondFactor(secret, recovery, code string) bool {
	if strings.TrimSpace(secret) == "" {
		return true
	}
	if code == "" {
		return false
	}
	if recovery != "" && subtle.ConstantTimeCompare([]byte(code), []byte(recovery)) == 1 {
		return true
	}
	return verifyTOTPAt(secret, code, time.Now())
}
