// Command totp generates an admin TOTP enrollment secret and a recovery code.
// Store both as Railway variables on the api service; the API never writes
// them to the database.
package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"flag"
	"fmt"

	"github.com/betva/backend/internal/httpapi"
)

func main() {
	existing := flag.String("secret", "", "print the current code for an existing secret")
	flag.Parse()
	if *existing != "" {
		code, err := httpapi.CurrentTOTP(*existing)
		if err != nil {
			panic(err)
		}
		fmt.Println(code)
		return
	}

	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)

	recoveryBytes := make([]byte, 16)
	if _, err := rand.Read(recoveryBytes); err != nil {
		panic(err)
	}
	recovery := hex.EncodeToString(recoveryBytes)

	code, err := httpapi.CurrentTOTP(secret)
	if err != nil {
		panic(err)
	}

	fmt.Println("Add this secret to your authenticator app:")
	fmt.Println(secret)
	fmt.Printf("\notpauth://totp/Betva:admin?secret=%s&issuer=Betva&algorithm=SHA1&digits=6&period=30\n", secret)
	fmt.Printf("\nCurrent code (for first login): %s\n", code)
	fmt.Printf("Recovery code (store offline): %s\n", recovery)
	fmt.Println("\nRailway variables: ADMIN_TOTP_SECRET and ADMIN_RECOVERY_CODE")
}
