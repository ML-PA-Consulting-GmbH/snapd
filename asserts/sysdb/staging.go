// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build withtestkeys || withstagingkeys
// +build withtestkeys withstagingkeys

/*
 * Copyright (C) 2016 Canonical Ltd
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

package sysdb

import (
	"fmt"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/constants"
)

func init() {
	stagingTrustedAccount, err := asserts.Decode([]byte(constants.EncodedStagingTrustedAccount))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	stagingRootAccountKey, err := asserts.Decode([]byte(constants.EncodedStagingRootAccountKey))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	trustedStagingAssertions = []asserts.Assertion{stagingTrustedAccount, stagingRootAccountKey}

	genericAccount, err := asserts.Decode([]byte(constants.EncodedStagingGenericAccount))
	if err != nil {
		panic(fmt.Sprintf(`cannot decode "generic"'s account: %v`, err))
	}
	genericModelsAccountKey, err := asserts.Decode([]byte(constants.EncodedStagingGenericModelsAccountKey))
	if err != nil {
		panic(fmt.Sprintf(`cannot decode "generic"'s "models" account-key: %v`, err))
	}

	genericStagingAssertions = []asserts.Assertion{genericAccount, genericModelsAccountKey}

	a, err := asserts.Decode([]byte(constants.EncodedStagingGenericClassicModel))
	if err != nil {
		panic(fmt.Sprintf(`cannot decode "generic"'s "generic-classic" model: %v`, err))
	}
	genericStagingClassicModel = a.(*asserts.Model)
}
