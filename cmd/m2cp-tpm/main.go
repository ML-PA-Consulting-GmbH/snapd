package main

import (
	"fmt"
	"github.com/snapcore/snapd/asserts"
	"os"
)

func main() {
	serial, err := asserts.TpmDeterministicDeviceSerial()
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}
	key, err := asserts.TpmGetEndorsementPublicKey()
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("m2cp signing key sha3-384..: %s\n", key.ID())
	fmt.Printf("deterministic device serial: %s\n", serial)
}
