// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"string"
	"github.com/snapcore/snapd/constants"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
)

var (
	trustedAssertions        []asserts.Assertion
	trustedStagingAssertions []asserts.Assertion
	trustedExtraAssertions   []asserts.Assertion
)

func init() {
	trustedAssertions = []asserts.Assertion{}
	accountAssertionsEncoded := strings.Split(constants.EncodedCanonicalAccount, "\n\n\n")
	for _, accountAssertionEncoded := range accountAssertionsEncoded {
		trimmed := strings.TrimSpace(accountAssertionEncoded) + "\n"
		accountAssertion, err := asserts.Decode([]byte(trimmed))
		if err != nil {
			panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
		}
		trustedAssertions = append(trustedAssertions, accountAssertion)
	}
	canonicalRootAccountKey, err := asserts.Decode([]byte(constants.EncodedCanonicalRootAccountKey))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	trustedAssertions = append(trustedAssertions, canonicalRootAccountKey)
}

// Trusted returns a copy of the current set of trusted assertions as used by Open.
func Trusted() []asserts.Assertion {
	trusted := []asserts.Assertion(nil)
	if !snapdenv.UseStagingStore() {
		trusted = append(trusted, trustedAssertions...)
	} else {
		if len(trustedStagingAssertions) == 0 {
			panic("cannot work with the staging store without a testing build with compiled-in staging keys")
		}
		trusted = append(trusted, trustedStagingAssertions...)
	}
	trusted = append(trusted, trustedExtraAssertions...)
	return trusted
}

// InjectTrusted injects further assertions into the trusted set for Open.
// Returns a restore function to reinstate the previous set. Useful
// for tests or called globally without worrying about restoring.
func InjectTrusted(extra []asserts.Assertion) (restore func()) {
	prev := trustedExtraAssertions
	trustedExtraAssertions = make([]asserts.Assertion, len(prev)+len(extra))
	copy(trustedExtraAssertions, prev)
	copy(trustedExtraAssertions[len(prev):], extra)
	return func() {
		trustedExtraAssertions = prev
	}
}
