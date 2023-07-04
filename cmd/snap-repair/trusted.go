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

package main

import (
	"fmt"
	"github.com/snapcore/snapd/constants"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
)

func init() {
	repairRootAccountKey, err := asserts.Decode([]byte(constants.GetEncoded("RepairRootAccountKey")))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted account-key: %v", err))
	}
	if !snapdenv.UseStagingStore() {
		trustedRepairRootKeys = append(trustedRepairRootKeys, repairRootAccountKey.(*asserts.AccountKey))
	}
}
