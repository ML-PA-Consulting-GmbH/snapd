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

package daemon_test

import (
	"bytes"
	"net/http/httptest"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/devicestate"
)

var _ = Suite(&liotSuite{})

type liotSuite struct {
	apiBaseSuite
}

const liotPath = "/v2/liot/provisioning/registration-data"

func (s *liotSuite) TestPostHappyPathStoresPayload(c *C) {
	s.expectRootAccess()
	d := s.daemonWithOverlordMock()

	body := []byte(`{
		"claim": {"token": "ABCD-1234-EFGH"},
		"hardware": {"machine_id": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
		"software": {"image": {"name": "uc-24", "version": "6.12"}},
		"collector": {"name": "liot-installer", "version": "1.0"}
	}`)
	req := httptest.NewRequest("POST", liotPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil, actionIsUnexpected)
	c.Check(rsp.Status, Equals, 200)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	stored, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Assert(stored, NotNil)
	c.Check(string(stored.Claim), Matches, `.*ABCD-1234-EFGH.*`)
	c.Check(stored.CollectedAt, Not(Equals), "")
}

func (s *liotSuite) TestPostInvalidJSONRejected(c *C) {
	s.expectRootAccess()
	s.daemonWithOverlordMock()

	req := httptest.NewRequest("POST", liotPath, strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")

	apiErr := s.errorReq(c, req, nil, actionIsUnexpected)
	c.Check(apiErr.Status, Equals, 400)
}

func (s *liotSuite) TestPostOverwritesExistingPayload(c *C) {
	s.expectRootAccess()
	d := s.daemonWithOverlordMock()

	st := d.Overlord().State()
	st.Lock()
	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: []byte(`{"token":"OLD"}`),
	})
	st.Unlock()

	req := httptest.NewRequest("POST", liotPath, strings.NewReader(`{"claim":{"token":"NEW"}}`))
	req.Header.Set("Content-Type", "application/json")
	rsp := s.syncReq(c, req, nil, actionIsUnexpected)
	c.Check(rsp.Status, Equals, 200)

	st.Lock()
	defer st.Unlock()
	stored, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Assert(stored, NotNil)
	c.Check(string(stored.Claim), Matches, `.*NEW.*`)
}

func (s *liotSuite) TestPostForgetClearsPayload(c *C) {
	s.expectRootAccess()
	d := s.daemonWithOverlordMock()

	st := d.Overlord().State()
	st.Lock()
	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: []byte(`{"token":"OLD"}`),
	})
	st.Unlock()

	req := httptest.NewRequest("POST", liotPath, strings.NewReader(`{"action":"forget"}`))
	req.Header.Set("Content-Type", "application/json")
	rsp := s.syncReq(c, req, nil, actionIsUnexpected)
	c.Check(rsp.Status, Equals, 200)

	st.Lock()
	defer st.Unlock()
	stored, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Check(stored, IsNil, Commentf("forget action must clear the partial payload"))
}

func (s *liotSuite) TestPostUnknownActionRejected(c *C) {
	s.expectRootAccess()
	s.daemonWithOverlordMock()

	req := httptest.NewRequest("POST", liotPath, strings.NewReader(`{"action":"detonate"}`))
	req.Header.Set("Content-Type", "application/json")
	apiErr := s.errorReq(c, req, nil, actionIsUnexpected)
	c.Check(apiErr.Status, Equals, 400)
}

// _ = daemon.RootAccess is referenced to surface a clear compile error if the
// access helper is renamed during refactors.
var _ daemon.AccessChecker = daemon.RootAccess{}
