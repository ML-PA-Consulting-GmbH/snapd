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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrUpdateEventsRejected indicates the store definitively rejected a batch of
// update events with a non-retryable (4xx) response. The batch will never be
// accepted as-is, so the caller must drop it rather than resend it forever.
var ErrUpdateEventsRejected = errors.New("store rejected update events")

const (
	updateEventsEndpointPath = "device/v3/update/events"
	// updateVersionsEndpointPath is the unversioned OTA discovery endpoint
	// (Transparent Update discovery §2.1). It advertises the URL majors the
	// backend serves and is deliberately version-less so it can be queried
	// before any versioned request is issued.
	updateVersionsEndpointPath = "device/update/versions"
)

// UpdateMajorOTA3 is the OTA URL major that carries the transparent-update
// events endpoint (device/v3/...). Events may only be reported once the backend
// advertises this major via SupportedUpdateMajors.
const UpdateMajorOTA3 = 3

// Update event components (Transparent Update Status Model §5). snapd runs on
// the device and only ever emits target events.
const (
	UpdateComponentTarget  = "target"
	UpdateComponentProxy   = "proxy"
	UpdateComponentBackend = "backend"
)

// Update event phases (Transparent Update Status Model §6.1).
const (
	UpdatePhasePreparation    = "preparation"
	UpdatePhaseActionReceived = "action_received"
	UpdatePhasePrecheck       = "precheck"
	UpdatePhaseDownload       = "download"
	UpdatePhaseVerify         = "verify"
	UpdatePhaseInstall        = "install"
	UpdatePhaseActivate       = "activate"
	UpdatePhaseReport         = "report"
)

// Update event status codes (Transparent Update Status Model §7.1). The action
// success (201) and cancellation (202) codes are backend-only and never emitted
// by snapd.
const (
	UpdateStatusProgress        = 150
	UpdateStatusPhaseSuccess    = 200
	UpdateStatusActionSuccess   = 201
	UpdateStatusActionCancelled = 202
	UpdateStatusRetryableError  = 400
	UpdateStatusFatalError      = 499
)

// UpdateMechanismSnap identifies snapd as the update mechanism in emitted events.
const UpdateMechanismSnap = "snap"

// UpdateEvent is a single transparent-update status report emitted by snapd
// (acting as the target component) during an update action. See the Transparent
// Update Status Model specification §4.
type UpdateEvent struct {
	// Required fields (spec §4.1).

	// UpdateActionID is the backend-assigned identifier of the update action
	// this event belongs to, as received in the snap refresh response.
	UpdateActionID string `json:"update_action_id"`
	// Component is the actor emitting the event. snapd always sets this to
	// UpdateComponentTarget.
	Component string `json:"component"`
	// Mechanism identifies the update mechanism, e.g. UpdateMechanismSnap.
	Mechanism string `json:"mechanism"`
	// Phase is the current update phase, one of the UpdatePhase* values.
	Phase string `json:"phase"`
	// StatusCode is the outcome of this event, one of the UpdateStatus* values.
	StatusCode int `json:"status_code"`
	// Timestamp is an RFC 3339 formatted timestamp.
	Timestamp string `json:"timestamp"`

	// Optional fields (spec §4.2).

	// OrderHint is a monotonically increasing per-UpdateActionID counter
	// starting at 0, used as a tiebreaker when events share a timestamp. It is
	// a pointer so that the zero value (0) is still transmitted while an unset
	// hint is omitted.
	OrderHint *int `json:"order_hint,omitempty"`
	// MechanismStatusCode is a mechanism-specific status code.
	MechanismStatusCode string `json:"mechanism_status_code,omitempty"`
	// Message is a free-text technical message from snapd.
	Message string `json:"message,omitempty"`
	// Details is an optional custom JSON payload for mechanism-specific data.
	Details map[string]any `json:"details,omitempty"`
}

// supportedUpdateMajorsResp matches the OTA discovery endpoint contract:
//
//	GET device/update/versions
//	→ 200 { "majors": [2, 3] }
//	→ 404                       (backend does not implement OTA discovery)
type supportedUpdateMajorsResp struct {
	Majors []int `json:"majors"`
}

// SupportedUpdateMajors queries the OTA discovery endpoint and returns the URL
// majors the backend serves (Transparent Update discovery §2.1). It is the gate
// for transparent-update reporting: events may only be sent when the returned
// list contains UpdateMajorOTA3.
//
// Return-value contract (mirrors the registration format-version probe):
//   - (majors, nil)  → authoritative verdict from a 200 response. The list may
//     be empty for a misconfigured backend; either way it is cacheable.
//   - (nil, nil)     → a definitive non-200 verdict (e.g. 404 endpoint not
//     implemented, 403 not whitelisted). The backend does not advertise OTA
//     majors here, so OTA3 is unavailable; cacheable.
//   - (nil, err)     → transient (network error, 5xx, 408, 429, undecodable
//     body). The caller MUST NOT cache and SHOULD retry later.
func (s *Store) SupportedUpdateMajors(ctx context.Context) ([]int, error) {
	url, err := s.endpointURL(updateVersionsEndpointPath, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot build update versions endpoint URL: %w", err)
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    url,
		Accept: jsonContentType,
	}

	var resp supportedUpdateMajorsResp
	httpResp, err := s.retryRequestDecodeJSON(ctx, reqOptions, nil, &resp, nil)
	if err != nil {
		// Transport error or undecodable body: transient, retry later.
		return nil, fmt.Errorf("cannot query supported update majors: %w", err)
	}

	switch {
	case httpResp.StatusCode == http.StatusOK:
		return resp.Majors, nil
	case httpResp.StatusCode >= 500, httpResp.StatusCode == http.StatusRequestTimeout, httpResp.StatusCode == http.StatusTooManyRequests:
		// Server error or retry-able 4xx: transient, like the registration
		// probe. Caller MUST NOT cache and SHOULD retry later.
		return nil, fmt.Errorf("cannot query supported update majors: %w", respToError(httpResp, "query supported update majors"))
	default:
		// Any other non-200 (404, 403, ...) is a definitive "no OTA majors
		// advertised here" verdict rather than an error to retry forever.
		return nil, nil
	}
}

// updateEventsRequest is the request body for the update events endpoint (spec §12.1).
type updateEventsRequest struct {
	Events []UpdateEvent `json:"events"`
}

// updateEventsError is a structured error response from the update events
// endpoint. It follows the store's standard error-list format, matching the
// messaging endpoint's error shape, so backend-side rejections (e.g. an unknown
// update_action_id or a malformed event) surface with their detail rather than
// only an HTTP status code.
type updateEventsError struct {
	ErrorList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error-list"`
}

func (e *updateEventsError) Error() string {
	msg := ""
	for i, err := range e.ErrorList {
		if i > 0 {
			msg += "; "
		}
		msg += fmt.Sprintf("%s (code: %s)", err.Message, err.Code)
	}
	return msg
}

// ReportUpdateEvents posts a batch of transparent-update events to the store's
// update events endpoint (Transparent Update Status Model §12). It uses the same
// device-session authentication as all other store requests. The store responds
// with 204 No Content on success. Reporting an empty batch is a no-op.
func (s *Store) ReportUpdateEvents(ctx context.Context, events []UpdateEvent) error {
	if len(events) == 0 {
		return nil
	}

	reqData, err := json.Marshal(updateEventsRequest{Events: events})
	if err != nil {
		return fmt.Errorf("cannot marshal update events request: %w", err)
	}

	url, err := s.endpointURL(updateEventsEndpointPath, nil)
	if err != nil {
		return fmt.Errorf("cannot build update events endpoint URL: %w", err)
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         url,
		ContentType: jsonContentType,
		Data:        reqData,
		Accept:      jsonContentType,
	}

	var errResp updateEventsError
	httpResp, err := s.retryRequestDecodeJSON(ctx, reqOptions, nil, nil, &errResp)
	if err != nil {
		return fmt.Errorf("cannot report update events: %w", err)
	}

	if httpResp.StatusCode != 204 {
		// Server errors and throttling/timeout are transient: the caller should
		// retry the same batch later (mirrors the discovery probe contract).
		if httpResp.StatusCode >= 500 || httpResp.StatusCode == http.StatusRequestTimeout || httpResp.StatusCode == http.StatusTooManyRequests {
			if len(errResp.ErrorList) > 0 {
				return fmt.Errorf("cannot report update events: %w (status: %d)", &errResp, httpResp.StatusCode)
			}
			return respToError(httpResp, "report update events")
		}
		// Any other non-204 (a 4xx, e.g. an expired/unknown update_action_id or
		// a malformed event) is a definitive rejection: resending the identical
		// batch will fail identically, so signal the caller to drop it via
		// ErrUpdateEventsRejected rather than retaining it forever.
		if len(errResp.ErrorList) > 0 {
			return fmt.Errorf("%w: %v (status: %d)", ErrUpdateEventsRejected, &errResp, httpResp.StatusCode)
		}
		return fmt.Errorf("%w: %v", ErrUpdateEventsRejected, respToError(httpResp, "report update events"))
	}

	return nil
}
