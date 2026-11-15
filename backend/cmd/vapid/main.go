package main

import (
	"fmt"
	"log"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Prints a fresh VAPID key pair. Put the values in VAPID_PUBLIC_KEY and
// VAPID_PRIVATE_KEY. Generate once per deployment and keep the private key
// secret; rotating it invalidates every existing push subscription.
func main() {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Fatalf("generate: %v", err)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", publicKey)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", privateKey)
}
