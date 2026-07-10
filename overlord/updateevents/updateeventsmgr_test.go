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

package updateevents_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/updateevents"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type mockStore struct {
	storetest.Store

	reportFn func(ctx context.Context, events []store.UpdateEvent) error
	majorsFn func(ctx context.Context) ([]int, error)

	reportedBatches [][]store.UpdateEvent
	majorsCalls     int
}

func (s *mockStore) ReportUpdateEvents(ctx context.Context, events []store.UpdateEvent) error {
	s.reportedBatches = append(s.reportedBatches, events)
	if s.reportFn != nil {
		return s.reportFn(ctx, events)
	}
	return nil
}

func (s *mockStore) SupportedUpdateMajors(ctx context.Context) ([]int, error) {
	s.majorsCalls++
	if s.majorsFn != nil {
		return s.majorsFn(ctx)
	}
	return []int{2, 3}, nil
}

type updateEventsSuite struct {
	testutil.BaseTest

	st    *state.State
	o     *overlord.Overlord
	mgr   *updateevents.UpdateEventsManager
	store *mockStore
}

var _ = Suite(&updateEventsSuite{})

func (s *updateEventsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()
	s.st = s.o.State()

	// A device context past seeding so the manager can reach the store.
	as := assertstest.FakeAssertion(map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: as.(*asserts.Model)}
	s.AddCleanup(snapstatetest.MockDeviceContext(deviceCtx))

	s.st.Lock()
	runner := s.o.TaskRunner()
	s.o.AddManager(runner)

	s.mgr = updateevents.Manager(s.st)
	s.o.AddManager(s.mgr)

	s.st.Set("seeded", true)

	s.store = &mockStore{}
	snapstate.ReplaceStore(s.st, s.store)
	s.st.Unlock()

	// StartUp registers the status handlers and itself takes the state lock,
	// so it must be called without holding it.
	err := s.o.StartUp()
	c.Assert(err, IsNil)
}

// newUpdateTask creates a task of the given kind in a fresh refresh change,
// carrying a SnapSetup with the given update action ID. Caller must hold the
// state lock.
func (s *updateEventsSuite) newUpdateTask(kind, actionID string) *state.Task {
	chg := s.st.NewChange("refresh", "refresh a snap")
	t := s.st.NewTask(kind, "summary for "+kind)
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo:       &snap.SideInfo{RealName: "foo", Revision: snap.R(2)},
		UpdateActionID: actionID,
	})
	chg.AddTask(t)
	return t
}

func (s *updateEventsSuite) TestStatusChangeGeneratesEvents(c *C) {
	fixedTime := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	defer updateevents.MockTimeNow(func() time.Time { return fixedTime })()

	s.st.Lock()
	defer s.st.Unlock()

	t := s.newUpdateTask("download-snap", "act-1")

	// A phase reports both its start (150 progress) and completion (200 phase
	// success), so the backend can tell a stuck phase (150 with no 200) apart
	// from a completed one.
	t.SetStatus(state.DoingStatus)
	t.SetStatus(state.DoneStatus)

	pending := s.mgr.PendingEvents()
	c.Assert(pending, HasLen, 2)

	hint0, hint1 := 0, 1
	c.Check(pending[0], DeepEquals, store.UpdateEvent{
		UpdateActionID: "act-1",
		Component:      store.UpdateComponentTarget,
		Mechanism:      store.UpdateMechanismSnap,
		Phase:          store.UpdatePhaseDownload,
		StatusCode:     store.UpdateStatusProgress,
		Timestamp:      fixedTime.Format(time.RFC3339),
		OrderHint:      &hint0,
		Message:        "summary for download-snap",
	})
	c.Check(pending[1].StatusCode, Equals, store.UpdateStatusPhaseSuccess)
	c.Check(*pending[1].OrderHint, Equals, hint1)
}

func (s *updateEventsSuite) TestDownloadProgressSampling(c *C) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	defer updateevents.MockTimeNow(func() time.Time { return now })()

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoingStatus)
	t.SetProgress("foo", 100, 1000)
	// Drop the phase-start (150) event so only sampled progress is observed.
	s.mgr.SetPending(nil, nil)
	s.st.Unlock()

	// Sampled before the first-delay elapses: nothing emitted yet.
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.reportedBatches, HasLen, 0)

	// After the first-delay a single progress event is emitted, with the speed
	// measured since the download was first seen (300 bytes over 30s = 10 B/s).
	now = now.Add(updateevents.DownloadProgressFirstDelay)
	s.st.Lock()
	t.SetProgress("foo", 400, 1000)
	s.st.Unlock()

	c.Assert(s.mgr.Ensure(), IsNil)
	c.Assert(s.store.reportedBatches, HasLen, 1)
	c.Assert(s.store.reportedBatches[0], HasLen, 1)
	ev := s.store.reportedBatches[0][0]
	c.Check(ev.Phase, Equals, store.UpdatePhaseDownload)
	c.Check(ev.StatusCode, Equals, store.UpdateStatusProgress)
	c.Check(ev.Message, Matches, `Downloading "foo": 40% at .*`)
	c.Check(ev.Details["progress_percent"], Equals, 40.0)
	c.Check(ev.Details["speed_bytes"], Equals, int64(10))

	// Before the cadence interval elapses again: no further event.
	now = now.Add(updateevents.DownloadProgressInterval - time.Second)
	s.st.Lock()
	t.SetProgress("foo", 500, 1000)
	s.st.Unlock()
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.reportedBatches, HasLen, 1)

	// Once the interval elapses, a second progress event is emitted.
	now = now.Add(time.Second)
	s.st.Lock()
	t.SetProgress("foo", 700, 1000)
	s.st.Unlock()
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Assert(s.store.reportedBatches, HasLen, 2)
	c.Check(s.store.reportedBatches[1][0].Details["progress_percent"], Equals, 70.0)
}

func (s *updateEventsSuite) TestDownloadProgressShortDownloadEmitsNothingExtra(c *C) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	defer updateevents.MockTimeNow(func() time.Time { return now })()

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoingStatus)
	t.SetProgress("foo", 100, 1000)
	s.mgr.SetPending(nil, nil)
	s.st.Unlock()

	// Sample repeatedly, but always within the first-delay window.
	for i := 0; i < 3; i++ {
		now = now.Add(5 * time.Second)
		c.Assert(s.mgr.Ensure(), IsNil)
	}
	c.Check(s.store.reportedBatches, HasLen, 0)

	// The download completes before any intermediate event was due.
	s.st.Lock()
	t.SetProgress("foo", 1000, 1000)
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()
	now = now.Add(time.Minute)
	c.Assert(s.mgr.Ensure(), IsNil)

	// Only the phase-completion (200) event was reported; no 150 progress event.
	c.Assert(s.store.reportedBatches, HasLen, 1)
	c.Assert(s.store.reportedBatches[0], HasLen, 1)
	c.Check(s.store.reportedBatches[0][0].StatusCode, Equals, store.UpdateStatusPhaseSuccess)
}

func (s *updateEventsSuite) TestDownloadProgressSkipsBeforeProgressSet(c *C) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	defer updateevents.MockTimeNow(func() time.Time { return now })()

	s.st.Lock()
	// A download-snap task that has entered DoingStatus but whose download
	// meter has not started reporting yet. state.Task.Progress returns
	// ("", 1, 1) in this window, which naively reads as 100% done.
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoingStatus)
	label, done, total := t.Progress()
	c.Assert(label, Equals, "")
	c.Assert(done, Equals, 1)
	c.Assert(total, Equals, 1)
	// Drop the phase-start (150) event so only sampled progress is observed.
	s.mgr.SetPending(nil, nil)
	s.st.Unlock()

	// Even well past the first-delay, no progress event is emitted for a
	// download that has not begun reporting: no spurious 100% event.
	now = now.Add(updateevents.DownloadProgressFirstDelay + time.Minute)
	c.Assert(s.mgr.Ensure(), IsNil)

	s.st.Lock()
	c.Check(s.mgr.PendingEvents(), HasLen, 0)
	s.st.Unlock()
	c.Check(s.store.reportedBatches, HasLen, 0)
}

func (s *updateEventsSuite) TestStopUnregistersHandlers(c *C) {
	// Stop takes the state lock itself, so it must be called without holding it.
	s.mgr.Stop()

	s.st.Lock()
	defer s.st.Unlock()

	// A marker-task transition after Stop must not buffer any event, since the
	// status-change handlers have been unregistered.
	t := s.newUpdateTask("link-snap", "act-1")
	t.SetStatus(state.DoingStatus)
	t.SetStatus(state.DoneStatus)

	c.Check(s.mgr.PendingEvents(), HasLen, 0)
}

func (s *updateEventsSuite) TestStatusChangeMapsErrorToFatal(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.newUpdateTask("link-snap", "act-1")
	t.SetStatus(state.ErrorStatus)

	pending := s.mgr.PendingEvents()
	c.Assert(pending, HasLen, 1)
	c.Check(pending[0].Phase, Equals, store.UpdatePhaseActivate)
	c.Check(pending[0].StatusCode, Equals, store.UpdateStatusFatalError)
}

func (s *updateEventsSuite) TestTaskKindPhaseMapping(c *C) {
	for _, tc := range []struct {
		kind  string
		phase string
	}{
		{"prerequisites", store.UpdatePhasePreparation},
		{"prepare-snap", store.UpdatePhasePreparation},
		{"download-snap", store.UpdatePhaseDownload},
		{"validate-snap", store.UpdatePhaseVerify},
		{"stop-snap-services", store.UpdatePhaseInstall},
		{"remove-aliases", store.UpdatePhaseInstall},
		{"unlink-current-snap", store.UpdatePhaseInstall},
		{"mount-snap", store.UpdatePhaseInstall},
		{"copy-snap-data", store.UpdatePhaseInstall},
		{"setup-profiles", store.UpdatePhaseInstall},
		{"link-snap", store.UpdatePhaseActivate},
		{"auto-connect", store.UpdatePhaseActivate},
		{"set-auto-aliases", store.UpdatePhaseActivate},
		{"setup-aliases", store.UpdatePhaseActivate},
		{"run-hook", store.UpdatePhaseActivate},
		{"start-snap-services", store.UpdatePhaseActivate},
		{"cleanup", store.UpdatePhaseReport},
		{"check-rerefresh", store.UpdatePhaseReport},
		{"process-delayed-security-backend-effects", store.UpdatePhaseReport},
	} {
		s.st.Lock()
		t := s.newUpdateTask(tc.kind, "act-1")
		t.SetStatus(state.DoneStatus)
		pending := s.mgr.PendingEvents()
		s.mgr.SetPending(nil, nil)
		s.st.Unlock()

		c.Assert(pending, HasLen, 1, Commentf("kind %q", tc.kind))
		c.Check(pending[0].Phase, Equals, tc.phase, Commentf("kind %q", tc.kind))
		c.Check(pending[0].StatusCode, Equals, store.UpdateStatusPhaseSuccess, Commentf("kind %q", tc.kind))
	}
}

func (s *updateEventsSuite) TestStatusChangeIgnoresNonMarkerKind(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.newUpdateTask("some-internal-task", "act-1")
	t.SetStatus(state.DoneStatus)
	t.SetStatus(state.DoneStatus)

	c.Check(s.mgr.PendingEvents(), HasLen, 0)
}

func (s *updateEventsSuite) TestStatusChangeIgnoresMissingActionID(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.newUpdateTask("download-snap", "")
	t.SetStatus(state.DoneStatus)
	t.SetStatus(state.DoneStatus)

	c.Check(s.mgr.PendingEvents(), HasLen, 0)
}

func (s *updateEventsSuite) TestEnsureReportsWhenOTA3Available(c *C) {
	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Assert(s.store.reportedBatches, HasLen, 1)
	c.Check(s.store.reportedBatches[0], HasLen, 1)
	c.Check(s.store.reportedBatches[0][0].UpdateActionID, Equals, "act-1")

	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 0)
}

func (s *updateEventsSuite) TestEnsureRetainsWhenNoOTA3(c *C) {
	s.store.majorsFn = func(context.Context) ([]int, error) { return []int{2}, nil }

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// Backend does not advertise OTA3: nothing is reported, but the buffer is
	// retained (not dropped) - this may be a transitional state, and it will be
	// re-probed after backoff. Growth is bounded by the cap, not by dropping.
	c.Check(s.store.reportedBatches, HasLen, 0)

	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 1)
}

func (s *updateEventsSuite) TestEnsureRetainsOnTransientDiscoveryError(c *C) {
	s.store.majorsFn = func(context.Context) ([]int, error) {
		return nil, errors.New("network down")
	}

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(s.store.reportedBatches, HasLen, 0)

	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 1)
}

func (s *updateEventsSuite) TestEnsureRetainsOnTransientSendError(c *C) {
	s.store.reportFn = func(context.Context, []store.UpdateEvent) error {
		return errors.New("temporary failure")
	}

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(s.store.reportedBatches, HasLen, 1)

	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 1)
}

func (s *updateEventsSuite) TestEnsureRetainsWhenStoreOfflineOnDiscovery(c *C) {
	s.store.majorsFn = func(context.Context) ([]int, error) {
		return nil, store.ErrStoreOffline
	}

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// store.access=offline is reversible: nothing is reported, but the buffer is
	// retained (bounded by the cap) so it can be delivered once the store is back
	// online, rather than being lost.
	c.Check(s.store.reportedBatches, HasLen, 0)

	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 1)
}

func (s *updateEventsSuite) TestEnsureRetainsWhenStoreOfflineOnReport(c *C) {
	// Discovery succeeds (or is served from cache) and reports OTA3 support,
	// but the actual report attempt hits the offline store.
	s.store.reportFn = func(context.Context, []store.UpdateEvent) error {
		return store.ErrStoreOffline
	}

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(s.store.reportedBatches, HasLen, 1)

	// Offline is a temporary condition: the buffer is retained for delivery once
	// the store is back, not dropped.
	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 1)
}

func (s *updateEventsSuite) TestEnsureDropsWhenBackendRejects(c *C) {
	// The backend advertises OTA3 but definitively rejects the batch (a
	// non-retryable 4xx, surfaced as store.ErrUpdateEventsRejected).
	s.store.reportFn = func(context.Context, []store.UpdateEvent) error {
		return store.ErrUpdateEventsRejected
	}

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(s.store.reportedBatches, HasLen, 1)

	// A definitive rejection is not treated as a transient failure: the batch
	// is dropped instead of being resent forever.
	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 0)
}

func (s *updateEventsSuite) TestEnsureBufferCapEvictsOldestAction(c *C) {
	restore := updateevents.MockMaxPendingEvents(3)
	defer restore()

	// Backend does not advertise OTA3 so events are retained (not drained),
	// which isolates the cap behaviour.
	s.store.majorsFn = func(context.Context) ([]int, error) { return []int{2}, nil }

	s.st.Lock()
	s.mgr.SetPending([]store.UpdateEvent{
		{UpdateActionID: "act-1"}, {UpdateActionID: "act-1"},
		{UpdateActionID: "act-2"}, {UpdateActionID: "act-2"},
	}, nil)
	s.st.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()
	// The oldest whole action group (act-1) is evicted to get under the cap;
	// act-2's events are kept intact (no stranded 150/200 pairs).
	pending := s.mgr.PendingEvents()
	c.Assert(pending, HasLen, 2)
	for _, ev := range pending {
		c.Check(ev.UpdateActionID, Equals, "act-2")
	}
}

func (s *updateEventsSuite) TestEnsureBacksOffAfterFailure(c *C) {
	s.store.reportFn = func(context.Context, []store.UpdateEvent) error {
		return errors.New("temporary failure")
	}

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	// First pass attempts and fails, arming the backoff.
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.reportedBatches, HasLen, 1)

	// An immediate second pass must NOT re-attempt (still within backoff), so a
	// burst of new events cannot hammer a down uplink. EnsureBefore alone could
	// not enforce this - the nextAttempt gate does.
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.reportedBatches, HasLen, 1)

	// The buffer is still retained for a later retry.
	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.PendingEvents(), HasLen, 1)
}

func (s *updateEventsSuite) TestEnsureNoopWhenEmpty(c *C) {
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(s.store.majorsCalls, Equals, 0)
	c.Check(s.store.reportedBatches, HasLen, 0)
}

func (s *updateEventsSuite) TestEnsureCachesMajorsVerdict(c *C) {
	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	// First flush probes discovery and reports.
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.majorsCalls, Equals, 1)
	c.Check(s.store.reportedBatches, HasLen, 1)

	// A second action's event is flushed without re-probing discovery; the
	// verdict is served from the cache within its TTL.
	s.st.Lock()
	t2 := s.newUpdateTask("download-snap", "act-2")
	t2.SetStatus(state.DoneStatus)
	s.st.Unlock()

	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.majorsCalls, Equals, 1)
	c.Check(s.store.reportedBatches, HasLen, 2)
}

func (s *updateEventsSuite) TestEnsureFallsBackToStaleMajorsCache(c *C) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	defer updateevents.MockTimeNow(func() time.Time { return now })()

	s.st.Lock()
	t := s.newUpdateTask("download-snap", "act-1")
	t.SetStatus(state.DoneStatus)
	s.st.Unlock()

	// First flush probes discovery and caches the verdict.
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.majorsCalls, Equals, 1)
	c.Check(s.store.reportedBatches, HasLen, 1)

	// Expire the cache and make discovery fail (uplink down); the stale verdict
	// is reused so reporting still proceeds.
	now = now.Add(2 * updateevents.MajorsCacheTTL)
	s.store.majorsFn = func(context.Context) ([]int, error) {
		return nil, errors.New("uplink down")
	}

	s.st.Lock()
	t2 := s.newUpdateTask("download-snap", "act-2")
	t2.SetStatus(state.DoneStatus)
	s.st.Unlock()

	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(s.store.majorsCalls, Equals, 2)
	c.Check(s.store.reportedBatches, HasLen, 2)
}

func (s *updateEventsSuite) TestChangeReadyPrunesOrderHints(c *C) {
	s.st.Lock()

	// Seed an order-hint counter for an action and a leftover one that should
	// survive because no ready change references it.
	s.mgr.SetPending(nil, map[string]int{"act-1": 3, "act-other": 1})

	// A non-marker task carrying act-1; completing it makes the change ready,
	// which queues act-1 for pruning without generating any events.
	t := s.newUpdateTask("cleanup", "act-1")
	t.SetStatus(state.DoneStatus)
	c.Check(t.Change().Status().Ready(), Equals, true)
	s.st.Unlock()

	// Pruning is deferred to the Ensure pass.
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()
	c.Check(s.mgr.OrderHints(), DeepEquals, map[string]int{"act-other": 1})
}
