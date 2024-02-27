//go:build arm64

package devicestate

import (
	"github.com/snapcore/snapd/asserts"
)

// getDeviceSerial generates a deterministic serial number for the device from the TPM's Endorsement Key.
func getDeviceSerial() (string, error) {
	return asserts.TpmDeterministicDeviceSerial()
}
