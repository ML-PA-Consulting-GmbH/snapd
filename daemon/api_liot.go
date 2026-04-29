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

package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
)

// The L-IoT registration endpoint is POST-only by design: the only thing
// the external provisioning tool needs to do is hand snapd a partial
// registration payload. Querying back is intentionally unsupported —
// callers determine the registration outcome via /v2/model/serial (the
// universal "device is registered" signal) and snap changes / journalctl
// for in-flight debugging.
var liotProvisioningRegistrationDataCmd = &Command{
	Path:        "/v2/liot/provisioning/registration-data",
	POST:        postLiotRegistrationData,
	WriteAccess: rootAccess{},
}

// liotAppstoreURLCmd exposes the configured Appstore base URL.
//
// Why this exists: snapd's standard way to retrieve the store URL is
// `GET /v2/find?q=get-snapstore-url`, which is gated on the device having
// a serial assertion (snapd refuses with HTTP 500 "no device serial yet"
// otherwise). The L-IoT provisioning tool needs the URL *before* the
// device is registered (to poll the claim-status endpoint). This endpoint
// returns the same value that snapd would use for its own serial-request
// flow, without the serial precondition.
var liotAppstoreURLCmd = &Command{
	Path:       "/v2/liot/appstore-url",
	GET:        getLiotAppstoreURL,
	ReadAccess: openAccess{},
}

// liotAppstoreURLResponse is the GET payload — a single string field so
// the wire shape can grow without breaking clients.
type liotAppstoreURLResponse struct {
	URL string `json:"url"`
}

func getLiotAppstoreURL(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	url, err := c.d.overlord.DeviceManager().LiotResolveAppstoreURL(st)
	if err != nil {
		return InternalError("cannot resolve L-IoT Appstore URL: %v", err)
	}
	return SyncResponse(liotAppstoreURLResponse{URL: url})
}

// liotRegistrationPostBody mirrors the v1 registration request format. Fields
// owned by snapd (format_version, nonce, snap, attestation) are accepted to
// keep the wire format symmetric with the spec but are discarded — snapd
// injects the authoritative values at assembly time.
//
// The optional Action field switches the handler between two modes:
//   - "" (default) → store the payload (regular submission).
//   - "forget"     → wipe per-registration state and abort the in-flight
//     become-operational change. Used by the provisioning
//     tool when its claiming token has expired and it wants
//     to start over from a clean slate (typically followed
//     by a reboot from the tool side).
type liotRegistrationPostBody struct {
	Action string `json:"action,omitempty"`

	Claim       json.RawMessage `json:"claim,omitempty"`
	Hardware    json.RawMessage `json:"hardware,omitempty"`
	Software    json.RawMessage `json:"software,omitempty"`
	Collector   json.RawMessage `json:"collector,omitempty"`
	CollectedAt string          `json:"collected_at,omitempty"`

	// Accepted but ignored — snapd owns these fields.
	FormatVersion json.RawMessage `json:"format_version,omitempty"`
	Nonce         json.RawMessage `json:"nonce,omitempty"`
	Snap          json.RawMessage `json:"snap,omitempty"`
	Attestation   json.RawMessage `json:"attestation,omitempty"`
}

// postLiotRegistrationData accepts the partial registration payload from the
// external provisioning tool, persists it to state, and wakes the ensure
// loop so request-serial can proceed.
//
// The handler is deliberately lenient on submissions: re-POSTing while a
// registration is in flight overwrites the stored payload (the next retry
// picks it up); re-POSTing after registration is complete is a harmless
// no-op since the registration task will not run again.
//
// When `action: "forget"` is specified, all other fields are ignored;
// see devicestate.LiotForget for the semantics.
func postLiotRegistrationData(c *Command, r *http.Request, _ *auth.UserState) Response {
	var body liotRegistrationPostBody
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		return BadRequest("cannot decode L-IoT registration data: %v", err)
	}
	if dec.More() {
		return BadRequest("spurious content after L-IoT registration data")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch body.Action {
	case "":
		devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
			Claim:       body.Claim,
			Hardware:    body.Hardware,
			Software:    body.Software,
			Collector:   body.Collector,
			CollectedAt: body.CollectedAt,
		})
		st.EnsureBefore(0)
	case "forget":
		devicestate.LiotForget(st)
		st.EnsureBefore(0)
	default:
		return BadRequest("unknown L-IoT registration action %q", body.Action)
	}

	return SyncResponse(nil)
}
