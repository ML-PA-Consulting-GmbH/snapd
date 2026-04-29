// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package devicestate

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdtool"
)

// liotProvisioningToolPath is the well-known location of the L-IoT
// provisioning tool. Its presence is the signal that this image is meant to
// go through the L-IoT claim-and-register flow; if it is absent the device
// uses the legacy registration path and the await-liot-registration-data
// task is not scheduled.
//
// This gate is deliberately simple — a future revision can replace it with
// a model-grade or gadget-config check without changing the API surface.
const liotProvisioningToolPath = "/usr/bin/liot-provisioning"

// liotProvisioningToolPresent is indirected so tests can stub it without
// touching the real filesystem.
var liotProvisioningToolPresent = func() bool {
	return osutil.FileExists(liotProvisioningToolPath)
}

// LiotProvisioningToolPresent reports whether the L-IoT provisioning tool is
// installed on this image. Used by ensureOperational to decide whether to
// schedule the await-liot-registration-data task.
func LiotProvisioningToolPresent() bool {
	return liotProvisioningToolPresent()
}

// State keys for the L-IoT provisioning flow.
const (
	liotRegistrationDataStateKey       = "liot-registration-data"
	liotSupportedVersionsCacheStateKey = "liot-registration-supported-versions"
)

// registrationFormatVersionPath is the well-known discovery endpoint exposed
// by the Appstore. A 200 response carries the supported versions list; a 404
// definitively means the backend is legacy. The path is co-located with the
// serial endpoint host (same scheme + host + port).
const registrationFormatVersionPath = "/device/v3/registration/format-version"

// RegistrationFormat is the body shape selected for the outgoing serial
// request. Determined by the discovery probe (see selectRegistrationFormat).
type RegistrationFormat string

const (
	FormatLegacy RegistrationFormat = "legacy"
	FormatV1     RegistrationFormat = "v1"
)

// LiotRegistrationData is the partial payload supplied by the external
// provisioning tool. snapd-owned fields (format_version, nonce, snap.*,
// attestation.*) are not represented here — they are injected at assembly
// time in submitSerialRequest.
type LiotRegistrationData struct {
	Claim       json.RawMessage `json:"claim,omitempty"`
	Hardware    json.RawMessage `json:"hardware,omitempty"`
	Software    json.RawMessage `json:"software,omitempty"`
	Collector   json.RawMessage `json:"collector,omitempty"`
	CollectedAt string          `json:"collected_at,omitempty"`
}

// GetLiotRegistrationData returns the partial registration payload stored in
// state, or nil if none has been received yet.
func GetLiotRegistrationData(st *state.State) (*LiotRegistrationData, error) {
	var data LiotRegistrationData
	if err := st.Get(liotRegistrationDataStateKey, &data); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}

// SetLiotRegistrationData stores the partial registration payload in state.
// Callers must hold the state lock.
func SetLiotRegistrationData(st *state.State, data *LiotRegistrationData) {
	if data.CollectedAt == "" {
		data.CollectedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	st.Set(liotRegistrationDataStateKey, data)
}

// ClearLiotRegistrationData removes the stored payload and the cached probe
// verdict (e.g. after successful registration). Callers must hold the state
// lock.
func ClearLiotRegistrationData(st *state.State) {
	st.Set(liotRegistrationDataStateKey, nil)
	st.Set(liotSupportedVersionsCacheStateKey, nil)
}

// liotAwaitRetryInterval is how long the await task sleeps between checks
// when no payload is present yet. The POST handler also calls EnsureBefore(0)
// on accept, so this is just a safety net.
var liotAwaitRetryInterval = 30 * time.Second

// liotRegistrationBody mirrors the v1 JSON wire format defined in the
// Device-Registration-Request-Format specification. The fields are populated
// from the partial payload supplied by the external tool plus the values
// snapd injects (format_version, nonce, snap.assertions_b64, attestation.*).
type liotRegistrationBody struct {
	FormatVersion int             `json:"format_version"`
	CollectedAt   string          `json:"collected_at,omitempty"`
	Collector     json.RawMessage `json:"collector,omitempty"`
	Nonce         string          `json:"nonce"`

	Claim       json.RawMessage     `json:"claim,omitempty"`
	Snap        liotRegistrationSnap `json:"snap"`
	Attestation *liotAttestation    `json:"attestation,omitempty"`
	Hardware    json.RawMessage     `json:"hardware,omitempty"`
	Software    json.RawMessage     `json:"software,omitempty"`
}

type liotRegistrationSnap struct {
	AssertionsB64 string `json:"assertions_b64"`
}

type liotAttestation struct {
	TPM *liotAttestationTPM `json:"tpm,omitempty"`
}

type liotAttestationTPM struct {
	EKPubB64 string `json:"ek_pub_b64,omitempty"`
}

// liotEKLookup is the EK source used when assembling the v1 body. Indirected
// so tests can stub it without bringing up a TPM. Returns ("", nil) when no
// TPM is present; a non-nil error indicates a real retrieval failure.
var liotEKLookup = func() (string, error) {
	if !asserts.HasTpm() {
		return "", nil
	}
	return asserts.TpmGetEndorsementPublicKeyBase64()
}

// defaultSnapdCollectorPayload returns the partial used when snapd itself is
// the collector — i.e. the Appstore supports v1 but no external provisioning
// tool has supplied richer metadata. We populate only what snapd already has:
// the collector identity. Hardware and software inventory collection is a
// follow-up; for now we ship the minimal v1 envelope and let the rest of the
// fields stay absent.
func defaultSnapdCollectorPayload() *LiotRegistrationData {
	return &LiotRegistrationData{
		Collector:   json.RawMessage(fmt.Sprintf(`{"name":"snapd","version":%q}`, snapdtool.Version)),
		CollectedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// buildLiotRegistrationBody assembles the v1 JSON body. serialRequest is the
// stacked serial-request + model assertion stream (raw bytes), which becomes
// snap.assertions_b64. requestID is the nonce returned by the request-id
// endpoint and embedded in the serial-request assertion; the same value is
// used as the top-level nonce so the backend can correlate both layers.
func buildLiotRegistrationBody(data *LiotRegistrationData, requestID, serialRequest string) ([]byte, error) {
	body := liotRegistrationBody{
		FormatVersion: 1,
		CollectedAt:   data.CollectedAt,
		Collector:     data.Collector,
		Nonce:         requestID,
		Claim:         data.Claim,
		Snap: liotRegistrationSnap{
			AssertionsB64: base64.StdEncoding.EncodeToString([]byte(serialRequest)),
		},
		Hardware: data.Hardware,
		Software: data.Software,
	}

	ekPubBase64, err := liotEKLookup()
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve TPM EK: %v", err)
	}
	if ekPubBase64 != "" {
		body.Attestation = &liotAttestation{
			TPM: &liotAttestationTPM{EKPubB64: ekPubBase64},
		}
	}

	return json.Marshal(body)
}

// liotSupportedVersionsCache is what we store under
// liotSupportedVersionsCacheStateKey. ProbedURL is the serial-request URL the
// probe was made against; we re-probe if the URL changes (e.g. proxy / device
// service reconfigured).
type liotSupportedVersionsCache struct {
	ProbedURL string `json:"probed_url"`
	Versions  []int  `json:"versions"`
}

// liotProbeHTTPGet is indirected so tests can stub the probe without standing
// up an httptest server. Production binds it to client.Get.
var liotProbeHTTPGet = func(client *http.Client, url string) (*http.Response, error) {
	return client.Get(url)
}

// probeRegistrationFormatVersionResp matches the discovery endpoint contract:
//
//	GET /device/v3/registration/format-version
//	→ 200 { "supported_versions": [1] }
//	→ 404                                       (legacy backend)
type probeRegistrationFormatVersionResp struct {
	SupportedVersions []int `json:"supported_versions"`
}

// probeRegistrationFormatVersionURL derives the discovery URL from the serial
// endpoint URL: same scheme + host, swap the path. This matches the
// Appstore's promise that the probe is co-located with the serial endpoint.
func probeRegistrationFormatVersionURL(serialRequestURL string) (string, error) {
	u, err := url.Parse(serialRequestURL)
	if err != nil {
		return "", fmt.Errorf("cannot parse serial-request URL %q: %v", serialRequestURL, err)
	}
	u.Path = registrationFormatVersionPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// probeSupportedRegistrationVersions queries the discovery endpoint and
// returns the backend's supported registration body versions.
//
// Return-value contract:
//   - len > 0           → 200, JSON parsed, this is the authoritative list.
//   - len == 0, err nil → 404, the backend definitively does not support v1+
//     (caller falls back to legacy).
//   - err != nil        → transient (5xx, network, malformed JSON).
//     Caller MUST NOT cache and MUST retry.
func probeSupportedRegistrationVersions(client *http.Client, serialRequestURL string) ([]int, error) {
	probeURL, err := probeRegistrationFormatVersionURL(serialRequestURL)
	if err != nil {
		return nil, err
	}

	resp, err := liotProbeHTTPGet(client, probeURL)
	if err != nil {
		return nil, fmt.Errorf("cannot probe registration format-version endpoint: %v", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed probeRegistrationFormatVersionResp
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("cannot decode registration format-version response: %v", err)
		}
		// An empty list from a 200 response would be a misconfigured
		// backend — surface it as legacy rather than treating it as
		// transient. The caller will pick legacy. We return a non-nil
		// empty slice to keep the (len, nil) "definitive" contract.
		if parsed.SupportedVersions == nil {
			return []int{}, nil
		}
		return parsed.SupportedVersions, nil
	case http.StatusNotFound:
		// Definitive: backend does not implement the discovery
		// endpoint, hence does not implement v1+. Use legacy.
		return []int{}, nil
	default:
		return nil, fmt.Errorf("registration format-version probe returned unexpected status %d", resp.StatusCode)
	}
}

// probeWithLockReleased runs the discovery probe with the state lock
// released. The deferred re-lock ensures the caller's lock invariant is
// restored even if the probe panics. Caller MUST hold the state lock on
// entry; on return the state lock is held again.
func probeWithLockReleased(st *state.State, client *http.Client, serialRequestURL string) ([]int, error) {
	st.Unlock()
	defer st.Lock()
	return probeSupportedRegistrationVersions(client, serialRequestURL)
}

// supportsV1 reports whether 1 appears in the supported-versions list.
func supportsV1(versions []int) bool {
	for _, v := range versions {
		if v == 1 {
			return true
		}
	}
	return false
}

// SelectRegistrationFormat picks the wire format for the next serial-request
// against the given backend.
//
// We always consult the discovery endpoint (or its cached verdict) — even
// when no L-IoT partial payload is in state. The reason: if the Appstore
// supports v1, snapd uses it and acts as the collector itself, populating
// only the fields it already has (TPM EK, the assertion stream, the
// nonce). The external provisioning tool is *one* source of richer claim /
// hardware / software metadata, not a precondition for v1.
//
// Decision flow:
//
//  1. Cached probe verdict matches the current URL → use it.
//  2. Probe the discovery endpoint:
//     - error → return error so the caller can Retry; no caching.
//     - success → cache, then decide.
//
// Caching means the probe runs at most once per registration attempt. The
// cache is cleared together with the partial payload on success
// (ClearLiotRegistrationData) so a wiped + re-registered device probes again.
//
// Concurrency: the caller MUST hold the state lock. The probe (network I/O)
// runs with the lock RELEASED — holding the state lock during an HTTP call
// to the Appstore would block every other snapd API request for the
// duration of the probe (including `snap changes`).
func SelectRegistrationFormat(st *state.State, client *http.Client, serialRequestURL string) (RegistrationFormat, error) {
	var cached liotSupportedVersionsCache
	if err := st.Get(liotSupportedVersionsCacheStateKey, &cached); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if cached.ProbedURL == serialRequestURL && cached.Versions != nil {
		if supportsV1(cached.Versions) {
			return FormatV1, nil
		}
		return FormatLegacy, nil
	}

	// Release the state lock while the probe is in flight — it's an HTTP
	// call to the Appstore and may block for the client timeout if the
	// network or backend misbehaves. probeWithLockReleased uses defer so
	// the lock is restored even on panic.
	versions, probeErr := probeWithLockReleased(st, client, serialRequestURL)
	if probeErr != nil {
		return "", probeErr
	}

	st.Set(liotSupportedVersionsCacheStateKey, liotSupportedVersionsCache{
		ProbedURL: serialRequestURL,
		Versions:  versions,
	})
	if supportsV1(versions) {
		return FormatV1, nil
	}
	return FormatLegacy, nil
}

// LiotResolveAppstoreURL returns the Appstore base URL configured for this
// device. Resolution order matches snapd's own serial-request flow:
//
//  1. Gadget snap's "device-service.url" config, if set.
//  2. The default base URL baked into snapd's constants
//     (BaseUrlSnapcraftStagingApi or its production sibling, depending on
//     the snapdenv staging flag — see baseURL() in handlers_serial.go).
//
// This exists for the L-IoT provisioning tool, which needs the Appstore URL
// BEFORE the device has a serial assertion. The standard mechanism for
// retrieving the URL — `GET /v2/find?q=get-snapstore-url` — requires the
// device to be registered (returns 500 "no device serial yet" otherwise).
// Callers must hold the state lock.
func (m *DeviceManager) LiotResolveAppstoreURL(st *state.State) (string, error) {
	var gadgetName string
	if model, err := m.Model(); err == nil && model != nil {
		gadgetName = model.Gadget()
	} else if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", fmt.Errorf("cannot read model assertion: %v", err)
	}

	if gadgetName != "" {
		tr := config.NewTransaction(st)
		var svcURI string
		if err := tr.GetMaybe(gadgetName, "device-service.url", &svcURI); err != nil {
			return "", fmt.Errorf("cannot read gadget config %q: %v", gadgetName, err)
		}
		if svcURI != "" {
			return svcURI, nil
		}
	}

	return baseURL().String(), nil
}

// LiotForget wipes all per-registration L-IoT state and aborts any in-flight
// become-operational change so the next ensure pass can queue a clean one.
//
// Use case: the external provisioning tool's claiming token has expired in
// the Appstore, the user has regenerated it, and the simplest path to a
// clean registration is to start over. The tool POSTs `{"action":"forget"}`
// to /v2/liot/provisioning/registration-data and (typically) reboots; on the
// next boot the await-liot-registration-data task is back at the start of a
// fresh become-operational change, ready to receive the new payload.
//
// Concretely this:
//
//   - Clears liot-registration-data (the partial payload).
//   - Clears liot-registration-supported-versions (the probe verdict cache).
//   - Aborts the active become-operational change, if any. The aborted
//     change stays in `snap changes` history as Error/Abort, which is
//     expected — it is the breadcrumb that says "we gave up on this
//     attempt, the next one will start fresh".
//
// Callers must hold the state lock.
func LiotForget(st *state.State) {
	ClearLiotRegistrationData(st)
	for _, chg := range st.Changes() {
		if chg.Kind() == becomeOperationalChangeKind && !chg.IsReady() {
			chg.Abort()
		}
	}
}

// doAwaitLiotRegistrationData blocks the registration change until the
// external provisioning tool has POSTed a valid registration payload. The
// task's own Doing/Done/Error status is the only operator-visible signal —
// `snap changes` and `journalctl -u snapd` provide all the diagnostic detail.
func (m *DeviceManager) doAwaitLiotRegistrationData(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	data, err := GetLiotRegistrationData(st)
	if err != nil {
		return err
	}
	if data == nil {
		t.Logf("waiting for L-IoT registration data")
		return &state.Retry{After: liotAwaitRetryInterval}
	}

	return nil
}
