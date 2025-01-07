//go:build amd64

package devicestate

import (
	"github.com/google/uuid"
)

// getDeviceSerial only generates deterministic serials on ARM64 - on AMD64, it just returns a random UUID.
func getDeviceSerial() (string, error) {
	return uuid.New().String(), nil
}
