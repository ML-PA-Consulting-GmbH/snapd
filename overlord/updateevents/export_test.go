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

package updateevents

import (
	"time"

	"github.com/snapcore/snapd/store"
)

// MajorsCacheTTL exposes the discovery-verdict cache TTL for tests.
const MajorsCacheTTL = majorsCacheTTL

// Download-progress sampling cadence, exposed for tests.
const (
	DownloadProgressFirstDelay = downloadProgressFirstDelay
	DownloadProgressInterval   = downloadProgressInterval
)

// MockTimeNow replaces the time source used for event timestamps.
func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() { timeNow = old }
}

// MockMaxPendingEvents lowers the buffered-event cap for tests.
func MockMaxPendingEvents(n int) (restore func()) {
	old := maxPendingEvents
	maxPendingEvents = n
	return func() { maxPendingEvents = old }
}

// PendingEvents returns the buffered events. Caller must hold the state lock.
func (m *UpdateEventsManager) PendingEvents() []store.UpdateEvent {
	es, err := m.getState()
	if err != nil {
		panic(err)
	}
	return es.Pending
}

// OrderHints returns the per-action order_hint counters. Caller must hold the
// state lock.
func (m *UpdateEventsManager) OrderHints() map[string]int {
	es, err := m.getState()
	if err != nil {
		panic(err)
	}
	return es.OrderHints
}

// SetPending seeds the pending buffer and order-hint counters. Caller must hold
// the state lock.
func (m *UpdateEventsManager) SetPending(pending []store.UpdateEvent, orderHints map[string]int) {
	m.setState(&updateEventsState{Pending: pending, OrderHints: orderHints})
}
