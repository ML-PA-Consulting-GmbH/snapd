package asserts

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTpmEncodePubKey(t *testing.T) {
	ekPub, err := TpmGetEndorsementPublicKey()
	assert.NoError(t, err)

	pubEncoded, err := EncodePublicKey(ekPub)
	assert.NoError(t, err)

	fmt.Printf("X-Tpm-Ek: %s\n", string(pubEncoded))
	fmt.Printf("X-Tpm-Ek-Sha3-384: %s\n", ekPub.ID())
}

func TestTpmSignBytes(t *testing.T) {
	signature, err := TpmSignBytes([]byte("hello world"))
	assert.NoError(t, err)
	assert.NotNil(t, signature)
}

func TestTpmGetEndorsementPublicKey(t *testing.T) {
	pub, err := TpmGetEndorsementPublicKey()
	assert.NoError(t, err)
	assert.NotNil(t, pub)
}

func TestTpmPushEkWithHeader(t *testing.T) {
	ekPub, err := TpmGetEndorsementPublicKey()
	assert.NoError(t, err)
	assert.NotNil(t, ekPub)

	pubEncoded, err := encodeKeyBase64(ekPub)
	assert.NoError(t, err)
	assert.NotNil(t, pubEncoded)

	fmt.Printf("X-Tpm-Ek: %s\n", string(pubEncoded))
}

func TestGetDriveSerial(t *testing.T) {
	serial, err := getDriveSerial("/dev/sda")
	fmt.Println(serial)
	assert.NoError(t, err)
	assert.NotEmpty(t, serial)
}

func TestGetMacAddresses(t *testing.T) {
	macs, err := getMacAddresses()
	assert.NoError(t, err)
	assert.NotEmpty(t, macs)
	fmt.Println(macs)
	fmt.Println(err)
}
