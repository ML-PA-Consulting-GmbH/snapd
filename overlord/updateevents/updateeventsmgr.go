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

// Package updateevents implements the manager that reports transparent-update
// status events to the store. As the update mechanism runs, snapd (acting as
// the "target" component) emits phase-level progress events - the same progress
// a user sees in `snap tasks <change-id>` - for snaps whose refresh carried a
// backend-assigned update_action_id. Events are buffered in state and flushed
// to the store's update events endpoint, but only once the backend advertises
// OTA major 3 via the discovery endpoint. See the Transparent Update Status
// Model specification.
package updateevents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil/quantity"
)

const updateEventsStateKey = "update-events"

const (
	// downloadProgressFirstDelay is how long a download must run before its
	// first intermediate progress event is emitted. Short downloads finish
	// before this and never produce intermediate events.
	downloadProgressFirstDelay = 30 * time.Second
	// downloadProgressInterval is the cadence of subsequent progress events
	// once the first has been emitted.
	downloadProgressInterval = 60 * time.Second
)

// majorsCacheTTL bounds how long a supported-update-majors verdict from the OTA
// discovery endpoint is reused before re-probing. Within the TTL the cached
// verdict is used without a network round-trip; once stale we re-probe but fall
// back to the cached verdict when the uplink is unavailable.
const majorsCacheTTL = time.Hour

var timeNow = time.Now

// taskKindToPhase maps the task kinds of a snap refresh change to the
// transparent-update phases they represent (Transparent Update Status Model
// §6.1), each kind fitted to the closest existing phase. Kinds absent from the
// map produce no events, so internal bookkeeping tasks do not flood the buffer.
//
// run-hook is a single kind covering several hooks (pre-refresh, post-refresh,
// configure, check-health). The early pre-refresh hook aside, hooks run once the
// new revision is being made live, so the whole kind is bucketed as activate;
// the event's Message still carries the specific hook summary.
var taskKindToPhase = map[string]string{
	// Preparation: gather prerequisites and stage the new revision.
	"prerequisites": store.UpdatePhasePreparation,
	"prepare-snap":  store.UpdatePhasePreparation,
	// Download the new revision's blob.
	"download-snap": store.UpdatePhaseDownload,
	// Verify the downloaded revision's assertions.
	"validate-snap": store.UpdatePhaseVerify,
	// Install: swap the old revision out and lay the new one down.
	"stop-snap-services":  store.UpdatePhaseInstall,
	"remove-aliases":      store.UpdatePhaseInstall,
	"unlink-current-snap": store.UpdatePhaseInstall,
	"mount-snap":          store.UpdatePhaseInstall,
	"copy-snap-data":      store.UpdatePhaseInstall,
	"setup-profiles":      store.UpdatePhaseInstall,
	// Activate: make the new revision live (link, connections, aliases, hooks,
	// services).
	"link-snap":           store.UpdatePhaseActivate,
	"auto-connect":        store.UpdatePhaseActivate,
	"set-auto-aliases":    store.UpdatePhaseActivate,
	"setup-aliases":       store.UpdatePhaseActivate,
	"run-hook":            store.UpdatePhaseActivate,
	"start-snap-services": store.UpdatePhaseActivate,
	// Report: post-activation cleanup and follow-up bookkeeping.
	"cleanup":         store.UpdatePhaseReport,
	"check-rerefresh": store.UpdatePhaseReport,
	"process-delayed-security-backend-effects": store.UpdatePhaseReport,
}

// statusToCode maps a task status transition to a transparent-update status
// code (Transparent Update Status Model §7.1). The boolean reports whether the
// transition is one we emit an event for; intermediate/undo statuses are
// ignored.
//
// Both the start (DoingStatus → 150 progress) and the completion (DoneStatus →
// 200 phase success) of a phase are reported. The 150/200 pair is what makes the
// update transparent: a 150 that is never followed by a 200 tells the backend
// the update is stuck in that phase. ErrorStatus maps to 499 (terminal) - snapd
// only reaches it after exhausting its internal retries, so the phase has
// definitively failed.
func statusToCode(new state.Status) (int, bool) {
	switch new {
	case state.DoingStatus:
		// The phase has started and is making progress.
		return store.UpdateStatusProgress, true
	case state.DoneStatus:
		// The phase completed successfully.
		return store.UpdateStatusPhaseSuccess, true
	case state.ErrorStatus:
		// The phase failed; the update cannot proceed.
		return store.UpdateStatusFatalError, true
	default:
		return 0, false
	}
}

// updateEventsState is the persistent state of the manager.
type updateEventsState struct {
	// Pending is the FIFO buffer of events awaiting delivery. New events are
	// appended at the back; successfully delivered events are trimmed from the
	// front.
	Pending []store.UpdateEvent `json:"pending,omitempty"`

	// OrderHints maps an update_action_id to the next order_hint counter to
	// assign for that action. It provides a monotonically increasing per-action
	// tiebreaker for events sharing a timestamp. Entries are pruned when the
	// owning change becomes ready.
	OrderHints map[string]int `json:"order-hints,omitempty"`

	// PrunePending lists update_action_ids whose owning change has become ready
	// and whose OrderHints entry should be dropped. Pruning is deferred to the
	// next Ensure pass rather than done inline in the change handler: the
	// framework invokes the change-status handler before the final task-status
	// handler of the same transition, so pruning inline would reset the counter
	// just before the last event of the change is generated.
	PrunePending []string `json:"prune-pending,omitempty"`

	// Majors caches the OTA discovery verdict so the discovery endpoint is not
	// probed on every flush and so a verdict remains available when the uplink
	// is temporarily unavailable.
	Majors *majorsCache `json:"majors-cache,omitempty"`
}

// majorsCache is a cached OTA discovery verdict: the URL majors the backend
// advertises, together with the time it was probed. List may be nil/empty,
// meaning the backend definitively advertises no OTA majors.
type majorsCache struct {
	List   []int     `json:"list"`
	Probed time.Time `json:"probed"`
}

// downloadProgress is the in-memory sampling state of one in-progress
// download-snap task, used to throttle intermediate progress events and to
// derive transfer speed between samples. It is intentionally not persisted: a
// download that is interrupted by a snapd restart is restarted from scratch, so
// any stale sample would be meaningless.
type downloadProgress struct {
	// firstSeen is when the manager first observed the download running, used
	// to defer the first progress event by downloadProgressFirstDelay.
	firstSeen time.Time
	// emitted reports whether the first progress event has been sent.
	emitted bool
	// refTime and refBytes are the time and byte count of the last emitted
	// progress event, the reference point for the next speed computation.
	refTime  time.Time
	refBytes int
}

// UpdateEventsManager buffers and reports transparent-update status events.
type UpdateEventsManager struct {
	state *state.State

	taskHandlerID   int
	changeHandlerID int

	// downloads tracks per-task download progress sampling state, keyed by task
	// ID. Only accessed from Ensure under the state lock.
	downloads map[string]*downloadProgress
}

// Manager creates a new UpdateEventsManager.
func Manager(st *state.State) *UpdateEventsManager {
	return &UpdateEventsManager{
		state:     st,
		downloads: make(map[string]*downloadProgress),
	}
}

// StartUp implements StateStarterUp.StartUp. It registers the status-change
// handlers that translate task and change transitions into update events. The
// handlers run in the task runner context under the state lock and therefore
// must not perform any I/O - they only mutate the in-state buffer and schedule
// an ensure pass for the actual reporting.
func (m *UpdateEventsManager) StartUp() error {
	m.state.Lock()
	defer m.state.Unlock()

	m.taskHandlerID = m.state.AddTaskStatusChangedHandler(m.taskStatusChanged)
	m.changeHandlerID = m.state.AddChangeStatusChangedHandler(m.changeStatusChanged)

	return nil
}

// Stop unregisters the status-change handlers.
func (m *UpdateEventsManager) Stop() {
	m.state.Lock()
	defer m.state.Unlock()

	m.state.RemoveTaskStatusChangedHandler(m.taskHandlerID)
	m.state.RemoveChangeStatusChangedHandler(m.changeHandlerID)
}

// getState retrieves the manager state, initializing it if absent. Caller must
// hold the state lock.
func (m *UpdateEventsManager) getState() (*updateEventsState, error) {
	var es updateEventsState
	err := m.state.Get(updateEventsStateKey, &es)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	return &es, nil
}

// setState persists the manager state. Caller must hold the state lock.
func (m *UpdateEventsManager) setState(es *updateEventsState) {
	m.state.Set(updateEventsStateKey, es)
}

// taskStatusChanged records an update event when a marker task of a snap update
// transitions. It is a no-op for tasks that are not part of a tracked binary
// update (i.e. whose SnapSetup carries no update_action_id). Caller context:
// invoked by the task runner under the state lock; must not do I/O.
func (m *UpdateEventsManager) taskStatusChanged(t *state.Task, old, new state.Status) (remove bool) {
	phase, ok := taskKindToPhase[t.Kind()]
	if !ok {
		return false
	}

	code, ok := statusToCode(new)
	if !ok {
		return false
	}

	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		// Not all tasks of these kinds carry a SnapSetup; ignore quietly.
		return false
	}
	if snapsup.UpdateActionID == "" {
		// Either not a binary update or the backend did not assign an action
		// ID for it (e.g. assertion-only update): nothing to report.
		return false
	}

	es, err := m.getState()
	if err != nil {
		logger.Noticef("cannot load update-events state: %v", err)
		return false
	}

	hint := m.nextOrderHint(es, snapsup.UpdateActionID)
	es.Pending = append(es.Pending, store.UpdateEvent{
		UpdateActionID: snapsup.UpdateActionID,
		Component:      store.UpdateComponentTarget,
		Mechanism:      store.UpdateMechanismSnap,
		Phase:          phase,
		StatusCode:     code,
		Timestamp:      timeNow().UTC().Format(time.RFC3339),
		OrderHint:      &hint,
		Message:        t.Summary(),
	})
	m.setState(es)

	logger.Debugf("buffered transparent-update event for action %q: phase=%s status=%d (task %q %s)", snapsup.UpdateActionID, phase, code, t.Kind(), new)

	// Flush as soon as possible; the actual reporting happens in Ensure with
	// the state lock released.
	m.state.EnsureBefore(0)

	return false
}

// nextOrderHint returns and advances the per-action order_hint counter. Caller
// must hold the state lock.
func (m *UpdateEventsManager) nextOrderHint(es *updateEventsState, actionID string) int {
	if es.OrderHints == nil {
		es.OrderHints = make(map[string]int)
	}
	hint := es.OrderHints[actionID]
	es.OrderHints[actionID] = hint + 1
	return hint
}

// changeStatusChanged marks the per-action order_hint counters of a change for
// pruning once it becomes ready, so the map does not grow without bound. The
// actual pruning is deferred to the next Ensure pass (see PrunePending). Caller
// context: invoked by the task runner under the state lock; must not do I/O.
func (m *UpdateEventsManager) changeStatusChanged(chg *state.Change, old, new state.Status) {
	if !new.Ready() {
		return
	}

	es, err := m.getState()
	if err != nil {
		logger.Noticef("cannot load update-events state: %v", err)
		return
	}
	if len(es.OrderHints) == 0 {
		return
	}

	marked := false
	for _, t := range chg.Tasks() {
		snapsup, err := snapstate.TaskSnapSetup(t)
		if err != nil || snapsup.UpdateActionID == "" {
			continue
		}
		if _, ok := es.OrderHints[snapsup.UpdateActionID]; ok {
			es.PrunePending = append(es.PrunePending, snapsup.UpdateActionID)
			marked = true
		}
	}

	if marked {
		m.setState(es)
		m.state.EnsureBefore(0)
	}
}

// pruneOrderHints drops the order_hint counters queued for pruning. Caller must
// hold the state lock.
func (m *UpdateEventsManager) pruneOrderHints(es *updateEventsState) {
	if len(es.PrunePending) == 0 {
		return
	}
	for _, actionID := range es.PrunePending {
		delete(es.OrderHints, actionID)
	}
	es.PrunePending = nil
	m.setState(es)
}

// sampleDownloads inspects in-progress download-snap tasks of tracked updates
// and appends throttled download-phase progress events (status 150) to the
// buffer. The first event for a download is deferred by
// downloadProgressFirstDelay and subsequent ones spaced by
// downloadProgressInterval, so short downloads emit nothing extra and long ones
// report at a bounded rate. Each event carries a human-readable Message and a
// Details payload with progress_percent and speed_bytes (bytes/second since the
// previous sample).
//
// It returns whether any download is still active (so the caller re-arms the
// ensure timer), whether it appended an event (so the caller persists state),
// and how long until the next sample is due. Caller must hold the state lock.
func (m *UpdateEventsManager) sampleDownloads(es *updateEventsState) (active, emitted bool, nextWake time.Duration) {
	now := timeNow()
	seen := make(map[string]bool)
	nextWake = downloadProgressInterval

	for _, chg := range m.state.Changes() {
		if chg.IsReady() {
			continue
		}
		for _, t := range chg.Tasks() {
			if t.Kind() != "download-snap" || t.Status() != state.DoingStatus {
				continue
			}
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil || snapsup.UpdateActionID == "" {
				continue
			}
			_, done, total := t.Progress()
			if total <= 0 {
				continue
			}

			active = true
			id := t.ID()
			seen[id] = true

			dp := m.downloads[id]
			if dp == nil {
				dp = &downloadProgress{firstSeen: now, refTime: now, refBytes: done}
				m.downloads[id] = dp
			}

			var untilDue time.Duration
			if !dp.emitted {
				untilDue = downloadProgressFirstDelay - now.Sub(dp.firstSeen)
			} else {
				untilDue = downloadProgressInterval - now.Sub(dp.refTime)
			}

			if untilDue <= 0 {
				secs := now.Sub(dp.refTime).Seconds()
				var speed int64
				if secs > 0 {
					speed = int64(float64(done-dp.refBytes) / secs)
				}
				percent := float64(done) / float64(total) * 100

				hint := m.nextOrderHint(es, snapsup.UpdateActionID)
				es.Pending = append(es.Pending, store.UpdateEvent{
					UpdateActionID: snapsup.UpdateActionID,
					Component:      store.UpdateComponentTarget,
					Mechanism:      store.UpdateMechanismSnap,
					Phase:          store.UpdatePhaseDownload,
					StatusCode:     store.UpdateStatusProgress,
					Timestamp:      now.UTC().Format(time.RFC3339),
					OrderHint:      &hint,
					Message:        fmt.Sprintf("Downloading %q: %.0f%% at %s", snapsup.InstanceName(), percent, strings.TrimSpace(quantity.FormatBPS(float64(done-dp.refBytes), secs, -1))),
					Details: map[string]any{
						"progress_percent": float64(int64(percent*10+0.5)) / 10,
						"speed_bytes":      speed,
					},
				})
				emitted = true

				dp.emitted = true
				dp.refTime = now
				dp.refBytes = done
				untilDue = downloadProgressInterval
			}

			if untilDue < nextWake {
				nextWake = untilDue
			}
		}
	}

	// Drop sampling state of downloads that are no longer running.
	for id := range m.downloads {
		if !seen[id] {
			delete(m.downloads, id)
		}
	}

	if nextWake < time.Second {
		nextWake = time.Second
	}
	return active, emitted, nextWake
}

// Ensure implements StateManager.Ensure. When events are pending it consults the
// store's OTA discovery endpoint (using a cached verdict within majorsCacheTTL,
// or falling back to a stale cached verdict when the uplink is unavailable) and,
// only if the backend advertises OTA major 3, reports the buffered events. The
// discovery probe is tri-state:
//
//   - transient failure (network error, 5xx, ...) → fall back to a cached
//     verdict if one exists, otherwise keep the buffer and retry.
//   - definitive "no OTA major 3" → drop the buffer; the backend will never
//     accept these events, so retaining them would leak memory unboundedly.
//   - OTA major 3 available → report; on success trim the delivered events, on
//     a transient send failure keep them for the next pass.
func (m *UpdateEventsManager) Ensure() error {
	m.state.Lock()

	es, err := m.getState()
	if err != nil {
		m.state.Unlock()
		return err
	}

	// Drop order-hint counters of changes that have become ready. This is done
	// here, outside the status-handler call stack, so it cannot race with the
	// generation of a change's final event.
	m.pruneOrderHints(es)

	// Sample in-progress downloads, appending throttled progress events, and
	// re-arm the ensure timer so sampling continues while a download runs.
	active, emitted, nextWake := m.sampleDownloads(es)
	if emitted {
		m.setState(es)
	}
	if active {
		m.state.EnsureBefore(nextWake)
	}

	if len(es.Pending) == 0 {
		m.state.Unlock()
		return nil
	}

	deviceCtx, err := snapstate.DevicePastSeeding(m.state, nil)
	if err != nil {
		// Device not ready yet (e.g. not seeded). Keep the buffer and retry on
		// a later ensure pass.
		m.state.Unlock()
		return nil
	}
	sto := snapstate.Store(m.state, deviceCtx)

	// Snapshot the events to send; the lock is released during the HTTP calls,
	// during which the status handlers may append further events at the back.
	events := make([]store.UpdateEvent, len(es.Pending))
	copy(events, es.Pending)

	// Read the cached discovery verdict while we still hold the lock.
	var majors, staleMajors []int
	haveFresh, haveStale := false, false
	if es.Majors != nil {
		staleMajors, haveStale = es.Majors.List, true
		if timeNow().Sub(es.Majors.Probed) < majorsCacheTTL {
			majors, haveFresh = es.Majors.List, true
		}
	}

	m.state.Unlock()

	ctx := context.TODO()

	if !haveFresh {
		probed, err := sto.SupportedUpdateMajors(ctx)
		switch {
		case err == nil:
			majors = probed
			m.cacheMajors(probed)
			logger.Debugf("backend advertises update majors %v; transparent-update reporting (OTA major %d) supported: %v", probed, store.UpdateMajorOTA3, containsMajor(probed, store.UpdateMajorOTA3))
		case haveStale:
			// Transient discovery failure but we have a cached verdict (uplink
			// unavailable): fall back to it rather than stalling reporting.
			logger.Debugf("cannot determine supported update majors, using cached verdict: %v", err)
			majors = staleMajors
		default:
			// Transient discovery failure and nothing cached: keep the buffer
			// and retry later.
			logger.Debugf("cannot determine supported update majors, will retry: %v", err)
			return nil
		}
	}

	if !containsMajor(majors, store.UpdateMajorOTA3) {
		// Definitive verdict: the backend does not serve OTA major 3, so it has
		// no update events endpoint. Drop the buffered events rather than
		// retaining them forever.
		logger.Debugf("store does not advertise OTA major %d; dropping %d buffered update event(s)", store.UpdateMajorOTA3, len(events))
		m.dropEvents(len(events))
		return nil
	}

	if err := sto.ReportUpdateEvents(ctx, events); err != nil {
		// Transient send failure: keep the buffer and retry later.
		logger.Debugf("cannot report update events, will retry: %v", err)
		return nil
	}

	logger.Debugf("reported %d transparent-update event(s) to the backend for action(s) %v", len(events), distinctActionIDs(events))

	m.dropEvents(len(events))

	return nil
}

// distinctActionIDs returns the unique update_action_ids present in events, in
// first-seen order, for logging.
func distinctActionIDs(events []store.UpdateEvent) []string {
	seen := make(map[string]bool, len(events))
	var ids []string
	for _, e := range events {
		if seen[e.UpdateActionID] {
			continue
		}
		seen[e.UpdateActionID] = true
		ids = append(ids, e.UpdateActionID)
	}
	return ids
}

// dropEvents removes the first n events from the pending buffer, i.e. the ones
// just delivered or discarded. Events appended at the back while the lock was
// released are preserved.
func (m *UpdateEventsManager) dropEvents(n int) {
	m.state.Lock()
	defer m.state.Unlock()

	es, err := m.getState()
	if err != nil {
		logger.Noticef("cannot load update-events state: %v", err)
		return
	}
	if n >= len(es.Pending) {
		es.Pending = nil
	} else {
		es.Pending = es.Pending[n:]
	}
	m.setState(es)
}

// cacheMajors records the latest OTA discovery verdict together with the time
// it was probed. Caller must NOT hold the state lock.
func (m *UpdateEventsManager) cacheMajors(majors []int) {
	m.state.Lock()
	defer m.state.Unlock()

	es, err := m.getState()
	if err != nil {
		logger.Noticef("cannot load update-events state: %v", err)
		return
	}
	es.Majors = &majorsCache{List: majors, Probed: timeNow()}
	m.setState(es)
}

func containsMajor(majors []int, want int) bool {
	for _, m := range majors {
		if m == want {
			return true
		}
	}
	return false
}
