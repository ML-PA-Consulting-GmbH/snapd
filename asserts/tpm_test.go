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
