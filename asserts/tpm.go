package asserts

import (
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"log"
	"sync"

	"github.com/google/go-tpm-tools/client"
	"github.com/google/go-tpm/legacy/tpm2"
	"github.com/google/go-tpm/tpmutil"
	"github.com/google/uuid"
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
		KeyBits: 2048,
	},
}

const tpmSigningKeyHandle = tpmutil.Handle(0x81010200) // m2cp defined handle

// TpmSignBytes signs the given bytes using the m2cp key (derived from EK).
func TpmSignBytes(toSign []byte) (signature []byte, err error) {
	var sig []byte

	if err = withTpm(func(key *client.Key) error {
		fmt.Printf("signing %d bytes with key %s, payload (base64 encoded):\n%s\n",
			len(toSign),
			RSAPublicKey(key.PublicKey().(*rsa.PublicKey)).ID(),
			base64.StdEncoding.EncodeToString(toSign),
		)

		digest := tpmHashBytes(toSign)
		fmt.Printf("Step 1: hashed the payload with SHA3_384. digest base64 encoded:\n%s\n", base64.StdEncoding.EncodeToString(digest))

		fmt.Printf("Step 2: sign the digest (hash again with sha2_256 and encrypt with RSA key)\n")
		sig, err = key.SignData(digest)
		if err != nil {
			return fmt.Errorf("failed to sign: %s", err)
		}

		fmt.Printf("Step 3: created signature, base64 encoded:\n%s\n", base64.StdEncoding.EncodeToString(sig))

		if !tpmVerifyEkSignature(key.PublicKey(), toSign, sig) {
			return fmt.Errorf("signature verification failed")
		}

		id := RSAPublicKey(key.PublicKey().(*rsa.PublicKey)).ID()
		fmt.Printf("signed message with key %s\n", id)

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

var counter = 0

func TpmTest() {
	message := []byte(fmt.Sprintf("hello world #%d", counter))
	counter++
	fmt.Printf("Signing '%s'..\n", string(message))

	signature, err := TpmSignBytes(message)
	if err != nil {
		fmt.Printf("failed: %s\n", err)
	} else {
		fmt.Printf("\nsuccess\n%s\n", base64.StdEncoding.EncodeToString(signature))
	}
	fmt.Println()
}

func withTpm(f func(key *client.Key) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from panic: %v", r)
		}
	}()

	tpmLock.Lock()
	defer tpmLock.Unlock()

	log.Printf("opening TPM\n")
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

	err = f(key)
	if err != nil {
		log.Printf("tpm operation finished with error: %s", err)
	} else {
		log.Printf("tpm operation finished successfully")
	}

	return err
}

func tpmVerifyEkSignature(pubKey crypto.PublicKey, message, signature []byte) bool {
	message = tpmHashBytes(message)

	hashAlgo := crypto.SHA256

	hash := hashAlgo.New()
	hash.Write(message)
	digest := hash.Sum(nil)

	return rsa.VerifyPKCS1v15(pubKey.(*rsa.PublicKey), hashAlgo, digest, signature) == nil

}

func tpmHashBytes(toHash []byte) []byte {
	hash := crypto.SHA3_512.New()
	hash.Write(toHash)
	return hash.Sum(nil)
}
