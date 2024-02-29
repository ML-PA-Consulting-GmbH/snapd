package asserts

import (
	"crypto"
	"crypto/rsa"
	"fmt"
	"github.com/google/go-tpm-tools/client"
	"github.com/google/go-tpm/legacy/tpm2"
	"github.com/google/go-tpm/tpmutil"
	"github.com/google/uuid"
	"log"
	"sync"
)

var (
	tpmLock sync.Mutex
)

var tpmSigningKeyTemplate = tpm2.Public{
	Type:       tpm2.AlgRSA,
	NameAlg:    tpm2.AlgSHA256,
	Attributes: tpm2.FlagSignerDefault,
	RSAParameters: &tpm2.RSAParams{
		Sign: &tpm2.SigScheme{
			Alg:  tpm2.AlgRSASSA,
			Hash: tpm2.AlgSHA256,
		},
		KeyBits: 4096,
	},
}

const tpmSigningKeyHandle = tpmutil.Handle(0x81010200) // m2cp defined handle

// TpmSignBytes signs the given bytes using the m2cp key (derived from EK).
func TpmSignBytes(toSign []byte) (signature []byte, err error) {
	var sig []byte

	if err = withTpm(func(key *client.Key) error {
		sig, err = key.SignData(toSign)
		if err != nil {
			return fmt.Errorf("failed to sign: %s", err)
		}

		if !tpmVerifyEkSignature(key.PublicKey(), toSign, sig) {
			return fmt.Errorf("signature verification failed")
		}

		id := RSAPublicKey(key.PublicKey().(*rsa.PublicKey)).ID()
		fmt.Printf("signed message with key %s", id)

		return nil
	}); err != nil {
		return nil, err
	}

	return sig, nil
}

// TpmGetEndorsementPublicKey returns the public key of m2cp key (derived from EK).
func TpmGetEndorsementPublicKey() (PublicKey, error) {
	var pub PublicKey

	if err := withTpm(func(key *client.Key) error {
		pub = RSAPublicKey(key.PublicKey().(*rsa.PublicKey))
		return nil
	}); err != nil {
		return nil, err
	}
	return pub, nil
}

func TpmGetEndorsementPublicKeyBase64() (string, error) {
	ekPub, err := TpmGetEndorsementPublicKey()
	if err != nil {
		return "", err
	}
	return encodeKeyBase64(ekPub)
}

// TpmDeterministicDeviceSerial generates a deterministic serial number for the device from the m2cp key (derived from EK).
func TpmDeterministicDeviceSerial() (string, error) {
	var deviceUUID uuid.UUID
	if err := withTpm(func(key *client.Key) error {
		keyId := RSAPublicKey(key.PublicKey().(*rsa.PublicKey)).ID()
		namespace := uuid.NewSHA1(uuid.NameSpaceOID, []byte("m2cp-device-serial"))
		deviceUUID = uuid.NewSHA1(namespace, []byte(keyId))

		return nil
	}); err != nil {
		return "", err
	}
	return deviceUUID.String(), nil
}

func withTpm(f func(key *client.Key) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from panic: %v", r)
		}
	}()

	tpmLock.Lock()
	defer tpmLock.Unlock()

	log.Printf("opening TPM")
	rwc, err := tpm2.OpenTPM("/dev/tpmrm0")
	if err != nil {
		return fmt.Errorf("failed to open TPM: %s", err)
	}
	defer func() {
		_ = rwc.Close()
	}()

	log.Printf("fetching/creating key")
	key, err := client.NewCachedKey(rwc, tpm2.HandleEndorsement, tpmSigningKeyTemplate, tpmSigningKeyHandle)
	if err != nil {
		return fmt.Errorf("failed to create key: %s", err)
	}
	defer key.Close()

	log.Printf("ready for tpm operation")

	return f(key)
}

func tpmVerifyEkSignature(pubKey crypto.PublicKey, message, signature []byte) bool {
	hashAlgo := crypto.SHA256

	hash := hashAlgo.New()
	hash.Write(message)
	digest := hash.Sum(nil)

	return rsa.VerifyPKCS1v15(pubKey.(*rsa.PublicKey), hashAlgo, digest, signature) == nil

}
