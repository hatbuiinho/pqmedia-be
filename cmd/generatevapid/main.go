// generatevapid prints a fresh VAPID keypair to stdout in .env format.
// Run once per environment; paste the output into your .env file.
package main

import (
	"fmt"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate vapid:", err)
		os.Exit(1)
	}
	fmt.Printf("WEB_PUSH_VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("WEB_PUSH_VAPID_PRIVATE_KEY=%s\n", priv)
}
