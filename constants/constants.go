// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014,2015,2017 Canonical Ltd
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

/*
See: https://stackoverflow.com/questions/36703867/golang-preprocessor-like-c-style-compile-switch
for go precompiler options (old source!)

*/

package constants

// modie: 1) ubuntu defaults 2) custom values 3) etc/snapd.conf
// #3 should be deactivated for production

// parameter: dangerous yes/no allow/disallow --dangerous installation
// parameter: devmode yes/no allow/disallow --devmode installation

const (
	// see https://dashboard.snapcraft.io/docs/
	// XXX: Repeating "api/" here is cumbersome, but the next generation
	// of store APIs will probably drop that prefix (since it now
	// duplicates the hostname), and we may want to switch to v2 APIs
	// one at a time; so it's better to consider that as part of
	// individual endpoint paths.
	SearchEndpPath      = "api/v1/snaps/search"
	OrdersEndpPath      = "api/v1/snaps/purchases/orders"
	BuyEndpPath         = "api/v1/snaps/purchases/buy"
	CustomersMeEndpPath = "api/v1/snaps/purchases/customers/me"
	SectionsEndpPath    = "api/v1/snaps/sections"
	CommandsEndpPath    = "api/v1/snaps/names"
	// v2
	SnapActionEndpPath = "v2/snaps/refresh"
	SnapInfoEndpPath   = "v2/snaps/info"
	CohortsEndpPath    = "v2/cohorts"
	FindEndpPath       = "v2/snaps/find"

	DeviceNonceEndpPath   = "api/v1/snaps/auth/nonces"
	DeviceSessionEndpPath = "api/v1/snaps/auth/sessions"

	AssertionsPath = "v2/assertions"
)
