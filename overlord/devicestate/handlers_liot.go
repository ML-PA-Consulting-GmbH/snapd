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
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
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
// definitively means the backend is legacy.
const registrationFormatVersionPath = "device/v3/registration/format-version"

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

	Claim       json.RawMessage      `json:"claim,omitempty"`
	Snap        liotRegistrationSnap `json:"snap"`
	Attestation *liotAttestation     `json:"attestation,omitempty"`
	Hardware    json.RawMessage      `json:"hardware,omitempty"`
	Software    json.RawMessage      `json:"software,omitempty"`
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
	if !hasTPM() {
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

// probeRegistrationFormatVersionResp matches the discovery endpoint contract:
//
//	GET /device/v3/registration/format-version
//	→ 200 { "supported_versions": [1] }
//	→ 404                                       (legacy backend)
type probeRegistrationFormatVersionResp struct {
	SupportedVersions []int `json:"supported_versions"`
}

// probeSupportedRegistrationVersions queries the discovery endpoint at probeURL
// and returns the backend's supported registration body versions. probeURL is
// built in setURLs from the same base as request-id/devices, so it follows the
// proxy / custom device-service selection. The returned note is a short
// human-readable description of the outcome, suitable for both the journal and
// the request-serial task log.
//
// Return-value contract:
//   - len > 0           → 200, JSON parsed, this is the authoritative list.
//   - len == 0, err nil → a non-200 status that is a definitive verdict
//     (404 endpoint not implemented, 403 not whitelisted, other 4xx, or any
//     non-200 2xx). The backend does not offer the new format here, so it is
//     treated as legacy and the caller falls back to the snapd-native format.
//   - err != nil        → transient: a transport error (no HTTP response at
//     all, e.g. no network), a 5xx server error, a retry-able 4xx (408, 429),
//     or a malformed 200 body. Caller MUST NOT cache and MUST retry — these
//     may resolve on their own.
func probeSupportedRegistrationVersions(client *http.Client, probeURL string) (versions []int, note string, err error) {
	logger.Debugf("probing registration format-version endpoint %q", probeURL)

	resp, err := client.Get(probeURL)
	if err != nil {
		// No HTTP response at all (often no network yet). We cannot tell
		// which format the backend speaks, so this is transient: retry.
		note = fmt.Sprintf("registration format probe %q got no response from the backend (network down or backend unreachable): %v; will retry", probeURL, err)
		logger.Noticef("WARNING: %s", note)
		return nil, note, fmt.Errorf("cannot probe registration format-version endpoint: %v", err)
	}
	defer resp.Body.Close()

	// A 5xx is a server-side error and is transient: the backend may
	// recover, so we must not lock in (and cache) a legacy verdict on it.
	// 408 (request timeout) and 429 (too many requests) are retry-able 4xx
	// codes — same reasoning. Error out so the caller retries registration
	// later, just like a network error.
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests {
		note = fmt.Sprintf("registration format probe %q: HTTP %d (transient server error); will retry", probeURL, resp.StatusCode)
		logger.Noticef("WARNING: %s", note)
		return nil, note, fmt.Errorf("registration format-version probe returned transient error %d", resp.StatusCode)
	}

	// Any other non-200 response (the retry-able codes above are already
	// handled) is a definitive "the discovery endpoint does not give us a
	// versions list here" verdict. Rather than guessing or looping, we treat
	// the backend as legacy. Only a clean 200 carrying a versions list is
	// taken as v1 support. This covers 404 (endpoint not implemented), 403
	// (e.g. an nginx allowlist that does not expose the new path), and any
	// other 2xx that is not a plain 200.
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusNotFound:
			note = fmt.Sprintf("registration format probe %q: HTTP 404, backend does not implement the discovery endpoint; using legacy registration format", probeURL)
			logger.Noticef("%s", note)
		case http.StatusForbidden:
			note = fmt.Sprintf("registration format probe %q: HTTP 403 (forbidden); using legacy registration format", probeURL)
			logger.Noticef("WARNING: %s", note)
		default:
			note = fmt.Sprintf("registration format probe %q: unexpected HTTP %d; using legacy registration format", probeURL, resp.StatusCode)
			logger.Noticef("WARNING: %s", note)
		}
		return []int{}, note, nil
	}

	var parsed probeRegistrationFormatVersionResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		// A 200 that we cannot decode is a misbehaving backend. We do not
		// know the supported versions, so this is transient: retry.
		note = fmt.Sprintf("registration format probe %q returned HTTP %d with an undecodable body: %v; will retry", probeURL, resp.StatusCode, err)
		logger.Noticef("WARNING: %s", note)
		return nil, note, fmt.Errorf("cannot decode registration format-version response: %v", err)
	}
	// An empty list from a 200 response would be a misconfigured backend —
	// surface it as legacy rather than treating it as transient. We return a
	// non-nil empty slice to keep the (len, nil) "definitive" contract.
	if parsed.SupportedVersions == nil {
		note = fmt.Sprintf("registration format probe %q: HTTP %d with no supported_versions; using legacy registration format", probeURL, resp.StatusCode)
		logger.Noticef("%s", note)
		return []int{}, note, nil
	}
	note = fmt.Sprintf("registration format probe %q: backend supports versions %v", probeURL, parsed.SupportedVersions)
	logger.Noticef("%s", note)
	return parsed.SupportedVersions, note, nil
}

// probeWithLockReleased runs the discovery probe with the state lock
// released. The deferred re-lock ensures the caller's lock invariant is
// restored even if the probe panics. Caller MUST hold the state lock on
// entry; on return the state lock is held again.
func probeWithLockReleased(st *state.State, client *http.Client, probeURL string) ([]int, string, error) {
	st.Unlock()
	defer st.Lock()
	return probeSupportedRegistrationVersions(client, probeURL)
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
// The returned note is a short human-readable description of how the format
// was decided (probe outcome or cache hit), suitable for recording in the
// request-serial task log so it is visible via `snap tasks`/`snap change`.
func SelectRegistrationFormat(st *state.State, client *http.Client, probeURL string) (format RegistrationFormat, note string, err error) {
	var cached liotSupportedVersionsCache
	if err := st.Get(liotSupportedVersionsCacheStateKey, &cached); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", "", err
	}
	if cached.ProbedURL == probeURL && cached.Versions != nil {
		if supportsV1(cached.Versions) {
			note = fmt.Sprintf("using cached registration format verdict for %q: v1 (supported versions %v)", probeURL, cached.Versions)
			logger.Debugf("%s", note)
			return FormatV1, note, nil
		}
		note = fmt.Sprintf("using cached registration format verdict for %q: legacy (supported versions %v)", probeURL, cached.Versions)
		logger.Debugf("%s", note)
		return FormatLegacy, note, nil
	}

	// Release the state lock while the probe is in flight — it's an HTTP
	// call to the Appstore and may block for the client timeout if the
	// network or backend misbehaves. probeWithLockReleased uses defer so
	// the lock is restored even on panic.
	versions, note, probeErr := probeWithLockReleased(st, client, probeURL)
	if probeErr != nil {
		// Transient failure (no network, malformed 2xx response). We do
		// not cache and do not guess a format — the caller retries.
		logger.Noticef("cannot determine registration format for %q, will retry: %v", probeURL, probeErr)
		return "", note, probeErr
	}

	st.Set(liotSupportedVersionsCacheStateKey, liotSupportedVersionsCache{
		ProbedURL: probeURL,
		Versions:  versions,
	})
	if supportsV1(versions) {
		logger.Noticef("selected registration format for %q: v1 (new JSON registration body)", probeURL)
		return FormatV1, note, nil
	}
	logger.Noticef("selected registration format for %q: legacy (snapd-native assertion stream)", probeURL)
	return FormatLegacy, note, nil
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
