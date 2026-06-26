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

package store_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type storeEventsSuite struct {
	baseStoreSuite
}

var _ = Suite(&storeEventsSuite{})

func (s *storeEventsSuite) SetUpTest(c *C) {
	s.baseStoreSuite.SetUpTest(c)
}

func intPtr(i int) *int { return &i }

func (s *storeEventsSuite) TestReportUpdateEventsOK(c *C) {
	var gotBody []byte
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/device/v3/update/events")
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")

		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		gotBody = body

		w.WriteHeader(204)
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	events := []store.UpdateEvent{
		{
			UpdateActionID: "ua-123",
			Component:      store.UpdateComponentTarget,
			Mechanism:      store.UpdateMechanismSnap,
			Phase:          store.UpdatePhaseDownload,
			StatusCode:     store.UpdateStatusProgress,
			OrderHint:      intPtr(0),
			Timestamp:      "2026-04-08T14:01:00Z",
		},
		{
			UpdateActionID:      "ua-123",
			Component:           store.UpdateComponentTarget,
			Mechanism:           store.UpdateMechanismSnap,
			Phase:               store.UpdatePhaseInstall,
			StatusCode:          store.UpdateStatusPhaseSuccess,
			OrderHint:           intPtr(1),
			MechanismStatusCode: "link-snap-done",
			Message:             "snap linked",
			Timestamp:           "2026-04-08T14:03:00Z",
			Details: map[string]any{
				"current_version":  "1.0",
				"expected_version": "2.0",
			},
		},
	}

	err := sto.ReportUpdateEvents(s.ctx, events)
	c.Assert(err, IsNil)

	// verify the wire shape matches the specification
	var req struct {
		Events []map[string]any `json:"events"`
	}
	c.Assert(json.Unmarshal(gotBody, &req), IsNil)
	c.Assert(req.Events, HasLen, 2)

	c.Check(req.Events[0]["update_action_id"], Equals, "ua-123")
	c.Check(req.Events[0]["component"], Equals, "target")
	c.Check(req.Events[0]["mechanism"], Equals, "snap")
	c.Check(req.Events[0]["phase"], Equals, "download")
	c.Check(req.Events[0]["status_code"], Equals, float64(150))
	// order_hint of 0 must be transmitted, not omitted
	c.Check(req.Events[0]["order_hint"], Equals, float64(0))
	// optional fields left unset must be omitted
	_, hasMsg := req.Events[0]["message"]
	c.Check(hasMsg, Equals, false)
	_, hasDetails := req.Events[0]["details"]
	c.Check(hasDetails, Equals, false)

	c.Check(req.Events[1]["status_code"], Equals, float64(200))
	c.Check(req.Events[1]["message"], Equals, "snap linked")
	c.Check(req.Events[1]["mechanism_status_code"], Equals, "link-snap-done")
	details := req.Events[1]["details"].(map[string]any)
	c.Check(details["expected_version"], Equals, "2.0")
}

func (s *storeEventsSuite) TestReportUpdateEventsEmptyIsNoop(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Fatalf("unexpected request for empty batch")
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	c.Check(sto.ReportUpdateEvents(s.ctx, nil), IsNil)
	c.Check(sto.ReportUpdateEvents(s.ctx, []store.UpdateEvent{}), IsNil)
}

func (s *storeEventsSuite) TestReportUpdateEventsServerError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("{}"))
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	events := []store.UpdateEvent{{UpdateActionID: "ua-123"}}
	err := sto.ReportUpdateEvents(s.ctx, events)
	c.Assert(err, ErrorMatches, "cannot report update events: got unexpected HTTP status code 500 via POST to .*")
}

func (s *storeEventsSuite) TestReportUpdateEventsUnexpectedStatus(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// a 200 (rather than the expected 204) is treated as an error
		w.WriteHeader(200)
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	events := []store.UpdateEvent{{UpdateActionID: "ua-123"}}
	err := sto.ReportUpdateEvents(s.ctx, events)
	c.Assert(err, ErrorMatches, ".*got unexpected HTTP status code 200 via POST to .*")
}

func (s *storeEventsSuite) TestReportUpdateEventsStoreOffline(c *C) {
	mockServerURL, _ := url.Parse("http://store.example.local")
	dauthCtx := &testDauthContext{c: c, device: s.device, storeOffline: true}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	events := []store.UpdateEvent{{UpdateActionID: "ua-123"}}
	err := sto.ReportUpdateEvents(s.ctx, events)
	c.Assert(err, testutil.ErrorIs, store.ErrStoreOffline)
}

func (s *storeEventsSuite) TestSupportedUpdateMajorsOK(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/device/update/versions")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"majors": []int{2, 3}})
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	majors, err := sto.SupportedUpdateMajors(s.ctx)
	c.Assert(err, IsNil)
	c.Check(majors, DeepEquals, []int{2, 3})
}

func (s *storeEventsSuite) TestSupportedUpdateMajorsNotImplemented(c *C) {
	// A backend that does not implement OTA discovery (404) is a definitive
	// "OTA3 unavailable" verdict, not a transient error.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	majors, err := sto.SupportedUpdateMajors(s.ctx)
	c.Assert(err, IsNil)
	c.Check(majors, IsNil)
}

func (s *storeEventsSuite) TestSupportedUpdateMajorsTooManyRequestsIsTransient(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	majors, err := sto.SupportedUpdateMajors(s.ctx)
	c.Assert(err, NotNil)
	c.Check(majors, IsNil)
}

func (s *storeEventsSuite) TestSupportedUpdateMajorsServerError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("{}"))
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	majors, err := sto.SupportedUpdateMajors(s.ctx)
	c.Assert(err, ErrorMatches, "cannot query supported update majors: .*")
	c.Check(majors, IsNil)
}
