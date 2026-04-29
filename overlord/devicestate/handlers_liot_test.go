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
	"strings"

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

const fakeSerialURL = "https://api.example.test/api/v1/snaps/auth/devices"

// stubResp builds an *http.Response with the given status and body suitable
// for returning from a stubbed liotProbeHTTPGet.
func stubResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func (s *liotHelpersSuite) TestProbeRegistrationFormatVersionURL(c *C) {
	got, err := devicestate.ProbeRegistrationFormatVersionURL("https://api.example.test/api/v1/snaps/auth/devices?foo=bar")
	c.Assert(err, IsNil)
	c.Check(got, Equals, "https://api.example.test"+devicestate.RegistrationFormatVersionPath)
}

func (s *liotHelpersSuite) TestProbeReturns200Versions(c *C) {
	var calledURL string
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, url string) (*http.Response, error) {
		calledURL = url
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore()

	versions, err := devicestate.ProbeSupportedRegistrationVersions(nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(versions, DeepEquals, []int{1})
	c.Check(calledURL, Equals, "https://api.example.test"+devicestate.RegistrationFormatVersionPath)
}

func (s *liotHelpersSuite) TestProbeReturns404IsLegacy(c *C) {
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(404, ""), nil
	})
	defer restore()

	versions, err := devicestate.ProbeSupportedRegistrationVersions(nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Assert(versions, NotNil)
	c.Check(versions, HasLen, 0)
}

func (s *liotHelpersSuite) TestProbeServerErrorIsTransient(c *C) {
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(503, ""), nil
	})
	defer restore()

	_, err := devicestate.ProbeSupportedRegistrationVersions(nil, fakeSerialURL)
	c.Check(err, ErrorMatches, ".*unexpected status 503.*")
}

func (s *liotHelpersSuite) TestProbeNetworkErrorIsTransient(c *C) {
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	})
	defer restore()

	_, err := devicestate.ProbeSupportedRegistrationVersions(nil, fakeSerialURL)
	c.Check(err, ErrorMatches, ".*connection refused.*")
}

func (s *liotHelpersSuite) TestProbeMalformedJSONIsTransient(c *C) {
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(200, "not json"), nil
	})
	defer restore()

	_, err := devicestate.ProbeSupportedRegistrationVersions(nil, fakeSerialURL)
	c.Check(err, ErrorMatches, ".*cannot decode.*")
}

// --- Selector tests ----------------------------------------------------------

func (s *liotHelpersSuite) TestSelectRegistrationFormatV1WhenAppstoreSupportsItEvenWithoutTool(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	probeCalls := 0
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		probeCalls++
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore()

	// No L-IoT partial payload is set: snapd will be the collector.
	format, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(format, Equals, devicestate.FormatV1)
	c.Check(probeCalls, Equals, 1, Commentf("probe must run even without partial payload"))
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatLegacyWhenAppstoreIsLegacy(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(404, ""), nil
	})
	defer restore()

	format, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
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

	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(200, `{"supported_versions":[1,2]}`), nil
	})
	defer restore()

	format, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
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

	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(404, ""), nil
	})
	defer restore()

	format, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
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

	probeCalls := 0
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		probeCalls++
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore()

	first, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(first, Equals, devicestate.FormatV1)

	second, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(second, Equals, devicestate.FormatV1)

	c.Check(probeCalls, Equals, 1, Commentf("second call must be served from cache"))
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatRefreshesOnURLChange(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	probeCalls := 0
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		probeCalls++
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore()

	_, err := devicestate.SelectRegistrationFormat(st, nil, "https://old.example.test/serial")
	c.Assert(err, IsNil)

	_, err = devicestate.SelectRegistrationFormat(st, nil, "https://new.example.test/serial")
	c.Assert(err, IsNil)

	c.Check(probeCalls, Equals, 2, Commentf("URL change must invalidate the cache"))
}

func (s *liotHelpersSuite) TestSelectRegistrationFormatProbeErrorPropagates(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return nil, fmt.Errorf("kaboom")
	})
	defer restore()

	_, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Check(err, ErrorMatches, ".*kaboom.*")
}

func (s *liotHelpersSuite) TestClearLiotRegistrationDataAlsoClearsCache(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"X"}`),
	})

	probeCalls := 0
	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		probeCalls++
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore()

	// First probe populates the cache.
	_, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(probeCalls, Equals, 1)

	// Clear should drop both the partial payload AND the probe cache, so
	// the next call probes again.
	devicestate.ClearLiotRegistrationData(st)

	_, err = devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(probeCalls, Equals, 2, Commentf("ClearLiotRegistrationData must invalidate the probe cache"))
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

	restore := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore()
	_, err := devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)

	devicestate.LiotForget(st)

	data, err := devicestate.GetLiotRegistrationData(st)
	c.Assert(err, IsNil)
	c.Check(data, IsNil)

	// The probe cache should also be gone — same observable as
	// ClearLiotRegistrationData; this asserts LiotForget delegates.
	probeCalls := 0
	restore2 := devicestate.MockLiotProbeHTTPGet(func(_ *http.Client, _ string) (*http.Response, error) {
		probeCalls++
		return stubResp(200, `{"supported_versions":[1]}`), nil
	})
	defer restore2()
	devicestate.SetLiotRegistrationData(st, &devicestate.LiotRegistrationData{
		Claim: json.RawMessage(`{"token":"Y"}`),
	})
	_, err = devicestate.SelectRegistrationFormat(st, nil, fakeSerialURL)
	c.Assert(err, IsNil)
	c.Check(probeCalls, Equals, 1, Commentf("probe should run again after forget+resubmit"))
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
