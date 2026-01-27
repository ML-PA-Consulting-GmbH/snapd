// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2024 Canonical Ltd
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

package naming

import (
	"github.com/snapcore/snapd/branding"
)

var wellKnownSnapIDs map[string]string
var wellKnownInitialized bool

// InitWellKnownSnapIDs initializes the well-known snap IDs from branding config.
// Must be called after branding.LoadConfig().
func InitWellKnownSnapIDs() {
	if wellKnownInitialized {
		return
	}
	// With unified branding, staging and production use the same IDs
	wellKnownSnapIDs = map[string]string{
		"core":   branding.BrandConfig.SnapIDs.Core,
		"snapd":  branding.BrandConfig.SnapIDs.Snapd,
		"core18": branding.BrandConfig.SnapIDs.Core18,
		"core20": branding.BrandConfig.SnapIDs.Core20,
		"core22": branding.BrandConfig.SnapIDs.Core22,
		"core24": branding.BrandConfig.SnapIDs.Core24,
		"core26": branding.BrandConfig.SnapIDs.Core26,
	}
	wellKnownInitialized = true
}

// WellKnownSnapID returns the snap-id of well-known snaps (snapd, core*)
// given the snap name or the empty string otherwise.
func WellKnownSnapID(snapName string) string {
	return wellKnownSnapIDs[snapName]
}

func UseStagingIDs(staging bool) (restore func()) {
	// With unified branding, staging and production use the same IDs.
	// This function is kept for API compatibility but is now a no-op.
	return func() {}
}
