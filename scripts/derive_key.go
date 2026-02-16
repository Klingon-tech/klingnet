// derive_key.go prints the pubkey and address for a hex-encoded private key file.
// Usage: go run scripts/derive_key.go <keyfile>
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: derive_key <keyfile>")
		os.Exit(1)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keyHex := strings.TrimSpace(string(data))
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	key, err := crypto.PrivateKeyFromBytes(keyBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pub := key.PublicKey()
	addr := crypto.AddressFromPubKey(pub)
	fmt.Printf("pubkey=%s\n", hex.EncodeToString(pub))
	fmt.Printf("address=%s\n", addr.String())
}
