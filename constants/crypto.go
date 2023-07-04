package constants

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"golang.org/x/crypto/sha3"
	"io"
	"strings"
	"time"
)

const (
	maxEncodeLineLength = 76
	v1                  = 0x1
)

var (
	v1Header         = []byte{v1}
	v1FixedTimestamp = time.Date(2016, time.January, 1, 0, 0, 0, 0, time.UTC)
)

type openpgpPrivateKey struct {
	privk *packet.PrivateKey
}

type keyEncoder interface {
	keyEncode(w io.Writer) error
}

func (opgPrivK openpgpPrivateKey) PublicKey() PublicKey {
	return newOpenPGPPubKey(&opgPrivK.privk.PublicKey)
}

func (opgPrivK openpgpPrivateKey) keyEncode(w io.Writer) error {
	return opgPrivK.privk.Serialize(w)
}

type openpgpPubKey struct {
	pubKey   *packet.PublicKey
	sha3_384 string
}

func (opgPubKey *openpgpPubKey) ID() string {
	return opgPubKey.sha3_384
}

func (opgPubKey *openpgpPubKey) verify(content []byte, sig *packet.Signature) error {
	h := sig.Hash.New()
	h.Write(content)
	return opgPubKey.pubKey.VerifySignature(h, sig)
}

func (opgPubKey openpgpPubKey) keyEncode(w io.Writer) error {
	return opgPubKey.pubKey.Serialize(w)
}

type PublicKey interface {
	ID() string
	// verify verifies signature is valid for content using the key.
	verify(content []byte, sig *packet.Signature) error
	keyEncoder
}

type PrivateKey interface {
	PublicKey() PublicKey
	keyEncoder
}

// RSAPrivateKey returns a PrivateKey for database use out of a rsa.PrivateKey.
func RSAPrivateKey(privk *rsa.PrivateKey) PrivateKey {
	intPrivk := packet.NewRSAPrivateKey(v1FixedTimestamp, privk)
	return openpgpPrivateKey{intPrivk}
}

// EncodePublicKey serializes a public key, typically for embedding in an assertion.
func EncodePublicKey(pubKey PublicKey) ([]byte, error) {
	return encodeKey(pubKey, "public key")
}

func CalcDigest(f crypto.Hash, bytes []byte) ([]byte, error) {
	if f == crypto.SHA3_384 {
		h := sha3.New384()
		h.Write(v1Header)
		h.Write(bytes)
		return h.Sum(nil), nil
	} else {
		return nil, fmt.Errorf("unknown hash function")
	}
}

// EncodeDigest encodes the digest from hash algorithm to be put in an assertion header.
func EncodeDigest(f crypto.Hash, hashDigest []byte) (string, error) {
	var algo string
	if f == crypto.SHA512 {
		algo = "sha512"
	} else if f == crypto.SHA3_384 {
		algo = "sha3-384"
	} else {
		return "", fmt.Errorf("unknown hash function")
	}
	if len(hashDigest) != f.Size() {
		return "", fmt.Errorf("%s hash has invlaid size: %d (should be %d) bytes", algo, len(hashDigest), f.Size())
	}
	encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(hashDigest)))
	base64.RawURLEncoding.Encode(encoded, hashDigest)
	return string(encoded), nil
}

func encodeKey(key keyEncoder, kind string) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := key.keyEncode(buf)
	if err != nil {
		return nil, fmt.Errorf("cannot encode %s: %v", kind, err)
	}
	return encodeV1(buf.Bytes()), nil
}

func encodeV1(data []byte) []byte {
	b64Encoded := base64.StdEncoding.EncodeToString(append(v1Header, data...))
	return []byte(strings.Join(wrapLines(b64Encoded), "\n"))
}

func newOpenPGPPubKey(intPubKey *packet.PublicKey) *openpgpPubKey {
	h := sha3.New384()
	h.Write(v1Header)
	err := intPubKey.Serialize(h)
	if err != nil {
		panic("internal error: cannot compute public key sha3-384")
	}
	sha3_384, err := EncodeDigest(crypto.SHA3_384, h.Sum(nil))
	if err != nil {
		panic("internal error: cannot compute public key sha3-384")
	}
	return &openpgpPubKey{pubKey: intPubKey, sha3_384: sha3_384}
}

func wrapLines(str string) []string {
	var lines []string
	for i := 0; i < len(str); i += maxEncodeLineLength {
		end := i + maxEncodeLineLength
		if end > len(str) {
			end = len(str)
		}
		lines = append(lines, str[i:end])
	}
	return lines
}
