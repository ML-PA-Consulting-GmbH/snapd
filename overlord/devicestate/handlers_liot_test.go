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

package devicestate_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
)

type liotHelpersSuite struct{}

var _ = Suite(&liotHelpersSuite{})

func (s *liotHelpersSuite) TestGetLiotRegistrationDataMissing(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	data, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Check(data, IsNil)
}

func (s *liotHelpersSuite) TestSetThenGetLiotRegistrationData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim:    json.RawMessage(`{"token":"X"}`),
		Hardware: json.RawMessage(`{"machine_id":"abc"}`),
	})

	data, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Assert(data, NotNil)
	c.Check(string(data.Claim), Equals, `{"token":"X"}`)
	// CollectedAt is auto-populated when missing.
	c.Check(data.CollectedAt, Not(Equals), "")
}

func (s *liotHelpersSuite) TestClearLiotRegistrationData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})
	devicestate.ClearLiotRegistrationData(st)

	data, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Check(data, IsNil)
}

func (s *liotHelpersSuite) TestBuildLiotRegistrationBody(c *C) {
	restore := devicestate.MockLiotEKLookup(func() (string, error) {
		return "EK_FAKE_BASE64", nil
	})
	defer restore()

	data := &devicestate.LiotRegistrationData{
		Claim:       json.RawMessage(`{"token":"ABCD-1234-EFGH"}`),
		Hardware:    json.RawMessage(`{"machine_id":"abc"}`),
		Software:    json.RawMessage(`{"image":{"name":"uc-24","version":"6.12"}}`),
		Collector:   json.RawMessage(`{"name":"liot-installer","version":"1.0"}`),
		CollectedAt: "2026-04-26T13:42:29Z",
	}
	const requestID = "NONCE-1"
	const serialRequestStream = "type: serial-request\n..."

	body, err := devicestate.BuildLiotRegistrationBody(data, requestID, serialRequestStream)
	c.Assert(err, IsNil)

	var got map[string]any
	c.Assert(json.Unmarshal(body, &got), IsNil)
	c.Check(got["format_version"], Equals, float64(1))
	c.Check(got["nonce"], Equals, requestID)
	c.Check(got["collected_at"], Equals, "2026-04-26T13:42:29Z")

	snap := got["snap"].(map[string]any)
	expectedAssertions := base64.StdEncoding.EncodeToString([]byte(serialRequestStream))
	c.Check(snap["assertions_b64"], Equals, expectedAssertions)

	att := got["attestation"].(map[string]any)
	tpm := att["tpm"].(map[string]any)
	c.Check(tpm["ek_pub_b64"], Equals, "EK_FAKE_BASE64")

	claim := got["claim"].(map[string]any)
	c.Check(claim["token"], Equals, "ABCD-1234-EFGH")
}

func (s *liotHelpersSuite) TestBuildLiotRegistrationBodyOmitsAttestationWithoutTPM(c *C) {
	restore := devicestate.MockLiotEKLookup(func() (string, error) {
		return "", nil
	})
	defer restore()

	body, err := devicestate.BuildLiotRegistrationBody(&devicestate.LiotRegistrationData{}, "n", "stream")
	c.Assert(err, IsNil)

	var got map[string]any
	c.Assert(json.Unmarshal(body, &got), IsNil)
	_, hasAttestation := got["attestation"]
	c.Check(hasAttestation, Equals, false)
}

// --- Format-discovery tests --------------------------------------------------

// probeServer returns an httptest server whose discovery endpoint replies with
// the given status and body, and a pointer to the request counter so tests can
// assert how many times the probe was actually performed.
func probeServer(status int, body string) (*httptest.Server, *int) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	return srv, &calls
}

func (s *liotHelpersSuite) TestProbeReturns200Versions(c *C) {
	srv, _ := probeServer(200, `{"supported_versions":[1]}`)
	defer srv.Close()

	versions, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(versions, DeepEquals, []int{1})
}

func (s *liotHelpersSuite) TestProbeReturns404IsLegacy(c *C) {
	srv, _ := probeServer(404, "")
	defer srv.Close()

	versions, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Assert(versions, NotNil)
	c.Check(versions, HasLen, 0)
}

func (s *liotHelpersSuite) TestProbeReturns403IsLegacy(c *C) {
	// Some backends/proxies (e.g. nginx with an endpoint allowlist) answer
	// an unknown path with 403 instead of 404. We must treat this as legacy,
	// not as a transient error, otherwise registration loops forever.
	srv, _ := probeServer(403, "")
	defer srv.Close()

	versions, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Assert(versions, NotNil)
	c.Check(versions, HasLen, 0)
}

func (s *liotHelpersSuite) TestProbeNon200SuccessIsLegacy(c *C) {
	// Only a clean 200 with a versions list counts as v1 support. Any other
	// 2xx (e.g. 202 Accepted) is not the discovery payload we expect, so we
	// treat it as legacy rather than decoding it.
	for _, status := range []int{201, 202, 204} {
		srv, _ := probeServer(status, "")

		versions, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
		c.Assert(err, IsNil, Commentf("status %d should not error", status))
		c.Assert(versions, NotNil, Commentf("status %d", status))
		c.Check(versions, HasLen, 0, Commentf("status %d should be legacy", status))
		srv.Close()
	}
}

func (s *liotHelpersSuite) TestProbeServerErrorIsTransient(c *C) {
	// A 5xx is a server-side error and is transient: we must not cache a
	// legacy verdict on it. Error out so the task retries — the backend may
	// recover and start answering the discovery endpoint.
	srv, _ := probeServer(503, "")
	defer srv.Close()

	_, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
	c.Check(err, ErrorMatches, ".*transient error 503.*")
}

func (s *liotHelpersSuite) TestProbeRetryable4xxIsTransient(c *C) {
	// 408 (request timeout) and 429 (too many requests) are retry-able 4xx
	// codes. Like a 5xx, they must error out rather than caching a legacy
	// verdict, so the task retries instead of locking in the wrong format.
	for _, status := range []int{408, 429} {
		srv, _ := probeServer(status, "")

		_, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
		c.Check(err, ErrorMatches, fmt.Sprintf(".*transient error %d.*", status), Commentf("status %d should be transient", status))
		srv.Close()
	}
}

func (s *liotHelpersSuite) TestProbeNetworkErrorIsTransient(c *C) {
	// No backend reachable at all (here: a server we close before probing).
	srv, _ := probeServer(200, `{"supported_versions":[1]}`)
	url := srv.URL
	client := srv.Client()
	srv.Close()

	_, _, err := devicestate.ProbeSupportedRegistrationVersions(client, url)
	c.Check(err, ErrorMatches, ".*cannot probe registration format-version endpoint.*")
}

func (s *liotHelpersSuite) TestProbeMalformedJSONIsTransient(c *C) {
	srv, _ := probeServer(200, "not json")
	defer srv.Close()

	_, _, err := devicestate.ProbeSupportedRegistrationVersions(srv.Client(), srv.URL)
	c.Check(err, ErrorMatches, ".*cannot decode.*")
}

// --- Selector tests ----------------------------------------------------------

func (s *liotHelpersSuite) TestSelectRegistrationFormatV1WhenAppstoreSupportsItEvenWithoutTool(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	srv, probeCalls := probeServer(200, `{"supported_versions":[1]}`)
	defer srv.Close()

	// No L-IoT partial payload is set: snapd will be the collector.
	format, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(format, Equals, devicestate.FormatV1)
	c.Check(*probeCalls, Equals, 1, Commentf("probe must run even without partial payload"))
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatLegacyWhenAppstoreIsLegacy(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	srv, _ := probeServer(404, "")
	defer srv.Close()

	format, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(format, Equals, devicestate.FormatLegacy)
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatV1WhenSupported(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	srv, _ := probeServer(200, `{"supported_versions":[1,2]}`)
	defer srv.Close()

	format, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(format, Equals, devicestate.FormatV1)
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatLegacyOn404(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	srv, _ := probeServer(404, "")
	defer srv.Close()

	format, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(format, Equals, devicestate.FormatLegacy)
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatCachesVerdict(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	srv, probeCalls := probeServer(200, `{"supported_versions":[1]}`)
	defer srv.Close()

	first, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(first, Equals, devicestate.FormatV1)

	second, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(second, Equals, devicestate.FormatV1)

	c.Check(*probeCalls, Equals, 1, Commentf("second call must be served from cache"))
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatRefreshesOnURLChange(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	srv, probeCalls := probeServer(200, `{"supported_versions":[1]}`)
	defer srv.Close()

	// Same server, but a different URL string is a different cache key, so
	// the verdict must be re-probed.
	_, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL+"/a")
	c.Assert(err, IsNil)

	_, _, err = devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL+"/b")
	c.Assert(err, IsNil)

	c.Check(*probeCalls, Equals, 2, Commentf("URL change must invalidate the cache"))
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatProbeErrorPropagates(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	// A 5xx surfaces as a transient error that the caller must retry on.
	srv, _ := probeServer(500, "")
	defer srv.Close()

	_, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Check(err, ErrorMatches, ".*transient error 500.*")
}

func (s *liotHelpersSuite) TestClearLiotRegistrationDataAlsoClearsCache(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	srv, probeCalls := probeServer(200, `{"supported_versions":[1]}`)
	defer srv.Close()

	// First probe populates the cache.
	_, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(*probeCalls, Equals, 1)

	// Clear should drop both the partial payload AND the probe cache, so
	// the next call probes again.
	devicestate.ClearLiotRegistrationData(st)

	_, _, err = devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(*probeCalls, Equals, 2, Commentf("ClearLiotRegistrationData must invalidate the probe cache"))
}

// --- Provisioning-tool gate tests --------------------------------------------

func (s *liotHelpersSuite) TestLiotProvisioningToolPresentReflectsStub(c *C) {
	restorePresent := devicestate.MockLiotProvisioningToolPresent(func() bool { return true })
	c.Check(devicestate.LiotProvisioningToolPresent(), Equals, true)
	restorePresent()

	restoreAbsent := devicestate.MockLiotProvisioningToolPresent(func() bool { return false })
	c.Check(devicestate.LiotProvisioningToolPresent(), Equals, false)
	restoreAbsent()
}

func (s *liotHelpersSuite) TestDefaultSnapdCollectorPayloadProducesSnapdEnvelope(c *C) {
	restore := devicestate.MockLiotEKLookup(func() (string, error) { return "", nil })
	defer restore()

	body, err := devicestate.BuildLiotRegistrationBody(devicestate.DefaultSnapdCollectorPayload(), "nonce-X", "type: serial-request\n...")
	c.Assert(err, IsNil)

	var got map[string]any
	c.Assert(json.Unmarshal(body, &got), IsNil)
	c.Check(got["format_version"], Equals, float64(1))
	c.Check(got["nonce"], Equals, "nonce-X")
	c.Check(got["collected_at"], Not(Equals), "")

	collector, ok := got["collector"].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(collector["name"], Equals, "snapd")
	c.Check(collector["version"], Not(Equals), "")

	// snapd-as-collector partial does not set claim/hardware/software.
	for _, k := range []string{"claim", "hardware", "software"} {
		_, present := got[k]
		c.Check(present, Equals, false, Commentf("expected %q to be absent in snapd-as-collector body", k))
	}
}

// --- LiotForget tests --------------------------------------------------------

func (s *liotHelpersSuite) TestLiotForgetClearsPayloadAndCache(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	srv, probeCalls := probeServer(200, `{"supported_versions":[1]}`)
	defer srv.Close()
	_, _, err := devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(*probeCalls, Equals, 1)

	devicestate.LiotForget(st)

	data, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Check(data, IsNil)

	// The probe cache should also be gone — same observable as
	// ClearLiotRegistrationData; this asserts LiotForget delegates. Same
	// URL, so a re-probe only happens if the cached verdict was dropped.
	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"Y"}`),
	})
	_, _, err = devicestate.SelectRegistrationFormat(st, srv.Client(), srv.URL)
	c.Assert(err, IsNil)
	c.Check(*probeCalls, Equals, 2, Commentf("probe should run again after forget+resubmit"))
}

func (s *liotHelpersSuite) TestLiotForgetAbortsActiveBecomeOperational(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange(devicestate.BecomeOperationalChangeKind, "Initialize device")
	t := st.NewTask("await-liot-registration-data", "Await L-IoT registration data")
	chg.AddTask(t)
	c.Assert(chg.IsReady(), Equals, false)

	devicestate.LiotForget(st)

	// Abort flags the change; tasks transition on the next ensure pass,
	// but for our purposes we want to know the change is no longer "live".
	// IsReady becomes true once the runner has processed the abort, but
	// the abort flag itself is set immediately on tasks.
	abortRequested := false
	for _, task := range chg.Tasks() {
		if task.Status() == state.HoldStatus || task.Status() == state.AbortStatus {
			abortRequested = true
		}
	}
	c.Check(abortRequested, Equals, true, Commentf("expected become-operational tasks to be marked for abort"))
}

func (s *liotHelpersSuite) TestLiotForgetIgnoresFinishedChanges(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange(devicestate.BecomeOperationalChangeKind, "Initialize device")
	t := st.NewTask("await-liot-registration-data", "Await L-IoT registration data")
	chg.AddTask(t)
	t.SetStatus(state.DoneStatus)
	c.Assert(chg.IsReady(), Equals, true)

	// Should not panic, should not change a finished change.
	devicestate.LiotForget(st)
	c.Check(chg.IsReady(), Equals, true)
}
