// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/constants"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
)

var (
	genericAssertions        []asserts.Assertion
	genericStagingAssertions []asserts.Assertion
	genericExtraAssertions   []asserts.Assertion

	genericClassicModel         *asserts.Model
	genericStagingClassicModel  *asserts.Model
	genericClassicModelOverride *asserts.Model
)

func init() {
	genericAccount, err := asserts.Decode([]byte(constants.EncodedGenericAccount))
	if err != nil {
		panic(fmt.Sprintf(`cannot decode "generic"'s account: %v`, err))
	}
	genericModelsAccountKey, err := asserts.Decode([]byte(constants.EncodedGenericModelsAccountKey))
	if err != nil {
		panic(fmt.Sprintf(`cannot decode "generic"'s "models" account-key: %v`, err))
	}

	genericAssertions = []asserts.Assertion{genericAccount, genericModelsAccountKey}

	fmt.Sprintf("Decode assert: %s", constants.EncodedGenericClassicModel)
	a, err := asserts.Decode([]byte(constants.EncodedGenericClassicModel))
	if err != nil {
		panic(fmt.Sprintf(`cannot decode "generic"'s "generic-classic" model: %v`, err))
	}
	genericClassicModel = a.(*asserts.Model)
}

// Generic returns a copy of the current set of predefined assertions for the 'generic' authority as used by Open.
func Generic() []asserts.Assertion {
	generic := []asserts.Assertion(nil)
	if !snapdenv.UseStagingStore() {
		generic = append(generic, genericAssertions...)
	} else {
		generic = append(generic, genericStagingAssertions...)
	}
	generic = append(generic, genericExtraAssertions...)
	return generic
}

// InjectGeneric injects further predefined assertions into the set used Open.
// Returns a restore function to reinstate the previous set. Useful
// for tests or called globally without worrying about restoring.
func InjectGeneric(extra []asserts.Assertion) (restore func()) {
	prev := genericExtraAssertions
	genericExtraAssertions = make([]asserts.Assertion, len(prev)+len(extra))
	copy(genericExtraAssertions, prev)
	copy(genericExtraAssertions[len(prev):], extra)
	return func() {
		genericExtraAssertions = prev
	}
}

// GenericClassicModel returns the model assertion for the "generic"'s "generic-classic" fallback model.
func GenericClassicModel() *asserts.Model {
	if genericClassicModelOverride != nil {
		return genericClassicModelOverride
	}
	if !snapdenv.UseStagingStore() {
		return genericClassicModel
	} else {
		return genericStagingClassicModel
	}
}

// MockGenericClassicModel mocks the predefined generic-classic model returned by GenericClassicModel.
func MockGenericClassicModel(mod *asserts.Model) (restore func()) {
	prevOverride := genericClassicModelOverride
	genericClassicModelOverride = mod
	return func() {
		genericClassicModelOverride = prevOverride
	}
}
