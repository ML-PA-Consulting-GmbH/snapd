//go:build arm64

package devicestate

import (
	"crypto/x509"
	"fmt"
	"github.com/google/go-attestation/attest"
	"github.com/google/uuid"
)

// getDeviceSerial generates a deterministic serial number for the device from the TPM's Endorsement Key.
func getDeviceSerial() (string, error) {
	tpm, err := attest.OpenTPM(nil)
	if err != nil {
		return "", fmt.Errorf("failed to open TPM: %s", err)
	}
	defer tpm.Close()

	eks, err := tpm.EKs()
	if err != nil {
		return "", fmt.Errorf("failed to get EKs: %s", err)
	}

	if len(eks) == 0 {
		return "", fmt.Errorf("no EKs found on the TPM")
	}

	// For simplicity, just use the first EK's public part.
	ekPub := eks[0].Public
	pubDER, err := x509.MarshalPKIXPublicKey(ekPub)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	// Hash the public part to create a UUID v5 namespace for "m2cp-device-serial".
	namespace := uuid.NewSHA1(uuid.NameSpaceOID, []byte("m2cp-device-serial"))
	deviceUUID := uuid.NewSHA1(namespace, pubDER)

	return deviceUUID.String(), nil
}
