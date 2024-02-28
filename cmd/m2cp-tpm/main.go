package main

import (
	"encoding/base64"
	"fmt"
	"github.com/snapcore/snapd/asserts"
)

func main() {
	fmt.Print("Generating deterministic serial: ")
	serial, err := asserts.TpmDeterministicDeviceSerial()
	if err != nil {
		fmt.Printf("failed: %s\n", err)
	} else {
		fmt.Printf("success\n%s\n", serial)
	}
	fmt.Println()

	fmt.Print("Getting m2cp-public-key: ")
	key, err := asserts.TpmGetEndorsementPublicKey()
	if err != nil {
		fmt.Printf("failed: %s\n", err)
	} else {
		fmt.Printf("success\npub key sha3-384: %s\n", key.ID())
	}
	fmt.Println()

	fmt.Print("Signing 'hello-world'..\n")
	signature, err := asserts.TpmSignBytes([]byte("hello world"))
	if err != nil {
		fmt.Printf("failed: %s\n", err)
	} else {
		fmt.Printf("\nsuccess\n%s\n", base64.StdEncoding.EncodeToString(signature))
	}
	fmt.Println()
}
