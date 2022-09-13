// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/constants"
	"github.com/snapcore/snapd/snapdenv"
)

var (
	prodWellKnownSnapIDs = map[string]string{
		"core":   constants.ProdIdCore,
		"snapd":  constants.ProdIdSnapd,
		"core18": constants.ProdIdCore18,
		"core20": constants.ProdIdCore20,
		"core22": constants.ProdIdCore22,
	}

	stagingWellKnownSnapIDs = map[string]string{
		"core":   constants.StagingIdCore,
		"snapd":  constants.StagingIdSnapd,
		"core18": constants.StagingIdCore18,
		"core20": constants.StagingIdCore20,
	}
)

var wellKnownSnapIDs = prodWellKnownSnapIDs

func init() {
	if snapdenv.UseStagingStore() {
		wellKnownSnapIDs = stagingWellKnownSnapIDs
	}
}

// WellKnownSnapID returns the snap-id of well-known snaps (snapd, core*)
// given the snap name or the empty string otherwise.
func WellKnownSnapID(snapName string) string {
	return wellKnownSnapIDs[snapName]
}

func UseStagingIDs(staging bool) (restore func()) {
	old := wellKnownSnapIDs
	if staging {
		wellKnownSnapIDs = stagingWellKnownSnapIDs
	} else {
		wellKnownSnapIDs = prodWellKnownSnapIDs
	}
	return func() {
		wellKnownSnapIDs = old
	}
}
