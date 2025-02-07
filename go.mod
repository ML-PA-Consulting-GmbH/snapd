module github.com/snapcore/snapd

go 1.19

// maze.io/x/crypto/afis imported by github.com/snapcore/secboot/tpm2
replace maze.io/x/crypto => github.com/snapcore/maze.io-x-crypto v0.0.0-20190131090603-9b94c9afe066

require (
	github.com/canonical/go-efilib v0.3.1-0.20220815143333-7e5151412e93 // indirect
	github.com/canonical/go-sp800.90a-drbg v0.0.0-20210314144037-6eeb1040d6c3 // indirect
	github.com/canonical/go-tpm2 v0.0.0-20210827151749-f80ff5afff61
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e
	github.com/godbus/dbus v0.0.0-20190726142602-4481cbc300e2
	github.com/gorilla/mux v1.7.4-0.20190701202633-d83b6ffe499a
	github.com/gvalkov/golang-evdev v0.0.0-20191114124502-287e62b94bcb
	github.com/jessevdk/go-flags v1.5.1-0.20210607101731-3927b71304df
	github.com/juju/ratelimit v1.0.1
	github.com/mvo5/goconfigparser v0.0.0-20200803085309-72e476556adb
	// if below two libseccomp-golang lines are updated, one must also update packaging/ubuntu-14.04/rules
	github.com/mvo5/libseccomp-golang v0.9.1-0.20180308152521-f4de83b52afb // old trusty builds only
	github.com/seccomp/libseccomp-golang v0.9.2-0.20220502024300-f57e1d55ea18
	github.com/snapcore/bolt v1.3.2-0.20210908134111-63c8bfcf7af8
	github.com/snapcore/go-gettext v0.0.0-20191107141714-82bbea49e785
	github.com/snapcore/secboot v0.0.0-20230623151406-4d331d24f830
	golang.org/x/crypto v0.13.0
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sys v0.14.0
	golang.org/x/xerrors v0.0.0-20220609144429-65e65417b02f
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c
	gopkg.in/macaroon.v1 v1.0.0-20150121114231-ab3940c6c165
	gopkg.in/retry.v1 v1.0.3
	gopkg.in/tomb.v2 v2.0.0-20161208151619-d5d1b5820637
	gopkg.in/tylerb/graceful.v1 v1.2.15
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/go-tpm v0.9.0
	github.com/google/go-tpm-tools v0.4.2
	github.com/google/uuid v1.6.0
	github.com/stretchr/testify v1.8.3
)

require (
	github.com/canonical/go-sp800.108-kdf v0.0.0-20210314145419-a3359f2d21b9 // indirect
	github.com/canonical/tcglog-parser v0.0.0-20210824131805-69fa1e9f0ad2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/go-attestation v0.5.1 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/go-sev-guest v0.9.3 // indirect
	github.com/google/go-tdx-guest v0.2.3-0.20231011100059-4cf02bed9d33 // indirect
	github.com/google/logger v1.1.1 // indirect
	github.com/kr/pretty v0.2.2-0.20200810074440-814ac30b4b18 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pborman/uuid v1.2.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	go.mozilla.org/pkcs7 v0.0.0-20200128120323-432b2356ecb1 // indirect
	golang.org/x/term v0.12.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	maze.io/x/crypto v0.0.0-20190131090603-9b94c9afe066 // indirect
)
