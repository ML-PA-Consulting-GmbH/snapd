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

package naming_test

import (
	"github.com/snapcore/snapd/constants"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
)

type wellKnownSuite struct{}

var _ = Suite(&wellKnownSuite{})

func (s wellKnownSuite) TestWellKnownSnapID(c *C) {
	c.Check(naming.WellKnownSnapID("foo"), Equals, "")

	c.Check(naming.WellKnownSnapID("snapd"), Equals, constants.ProdIdSnapd)

	c.Check(naming.WellKnownSnapID("core"), Equals, constants.ProdIdCore)
	c.Check(naming.WellKnownSnapID("core18"), Equals, constants.ProdIdCore18)
	c.Check(naming.WellKnownSnapID("core20"), Equals, constants.ProdIdCore20)
}

func (s wellKnownSuite) TestWellKnownSnapIDStaging(c *C) {
	defer naming.UseStagingIDs(true)()

	c.Check(naming.WellKnownSnapID("baz"), Equals, "")

	c.Check(naming.WellKnownSnapID("snapd"), Equals, constants.StagingIdSnapd)

	c.Check(naming.WellKnownSnapID("core"), Equals, constants.StagingIdCore)
	c.Check(naming.WellKnownSnapID("core18"), Equals, constants.StagingIdCore18)
	// XXX no core20 uploaded to staging yet
	c.Check(naming.WellKnownSnapID("core20"), Equals, "")
}
