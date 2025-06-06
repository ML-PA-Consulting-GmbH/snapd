// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package wrappers

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/usersession/client"
)

type Interacter interface {
	Notify(status string)
}

// wait this time between TERM and KILL
var killWait = 5 * time.Second

func serviceStopTimeout(app *snap.AppInfo) time.Duration {
	tout := app.StopTimeout
	if tout == 0 {
		tout = timeout.DefaultTimeout
	}
	return time.Duration(tout)
}

func generateSnapServiceFile(app *snap.AppInfo, opts *generateSnapServicesOptions) ([]byte, error) {
	if err := snap.ValidateApp(app); err != nil {
		return nil, err
	}

	return genServiceFile(app, opts)
}

func min(a, b int) int {
	if b < a {
		return b
	}
	return a
}

func formatCpuGroupSlice(grp *quota.Group) string {
	header := `# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
`
	buf := bytes.NewBufferString(header)

	count, percentage := grp.GetLocalCPUQuota()
	if percentage != 0 {
		// convert the number of cores and the allowed percentage
		// to the systemd specific format.
		cpuQuotaSnap := count * percentage
		cpuQuotaMax := runtime.NumCPU() * 100

		// The CPUQuota setting is only available since systemd 213
		fmt.Fprintf(buf, "CPUQuota=%d%%\n", min(cpuQuotaSnap, cpuQuotaMax))
	}

	if grp.CPULimit != nil && len(grp.CPULimit.CPUSet) != 0 {
		allowedCpusValue := strutil.IntsToCommaSeparated(grp.CPULimit.CPUSet)
		fmt.Fprintf(buf, "AllowedCPUs=%s\n", allowedCpusValue)
	}

	buf.WriteString("\n")
	return buf.String()
}

func formatMemoryGroupSlice(grp *quota.Group) string {
	header := `# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
`
	buf := bytes.NewBufferString(header)
	if grp.MemoryLimit != 0 {
		valuesTemplate := `MemoryMax=%[1]d
# for compatibility with older versions of systemd
MemoryLimit=%[1]d

`
		fmt.Fprintf(buf, valuesTemplate, grp.MemoryLimit)
	}
	return buf.String()
}

func formatTaskGroupSlice(grp *quota.Group) string {
	header := `# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`
	buf := bytes.NewBufferString(header)

	if grp.ThreadLimit != 0 {
		fmt.Fprintf(buf, "TasksMax=%d\n", grp.ThreadLimit)
	}
	return buf.String()
}

// generateGroupSliceFile generates a systemd slice unit definition for the
// specified quota group.
func generateGroupSliceFile(grp *quota.Group) []byte {
	buf := bytes.Buffer{}

	cpuOptions := formatCpuGroupSlice(grp)
	memoryOptions := formatMemoryGroupSlice(grp)
	taskOptions := formatTaskGroupSlice(grp)
	template := `[Unit]
Description=Slice for snap quota group %[1]s
Before=slices.target
X-Snappy=yes

[Slice]
`

	fmt.Fprintf(&buf, template, grp.Name)
	fmt.Fprint(&buf, cpuOptions, memoryOptions, taskOptions)
	return buf.Bytes()
}

func formatJournalSizeConf(grp *quota.Group) string {
	if grp.JournalLimit.Size == 0 {
		return ""
	}
	return fmt.Sprintf(`SystemMaxUse=%[1]d
RuntimeMaxUse=%[1]d
`, grp.JournalLimit.Size)
}

func formatJournalRateConf(grp *quota.Group) string {
	if !grp.JournalLimit.RateEnabled {
		return ""
	}
	return fmt.Sprintf(`RateLimitIntervalSec=%dus
RateLimitBurst=%d
`, grp.JournalLimit.RatePeriod.Microseconds(), grp.JournalLimit.RateCount)
}

func generateJournaldConfFile(grp *quota.Group) []byte {
	if grp.JournalLimit == nil {
		return nil
	}

	sizeOptions := formatJournalSizeConf(grp)
	rateOptions := formatJournalRateConf(grp)
	// Set Storage=auto for all journal namespaces we create. This is
	// the setting for the default namespace, and 'persistent' is the default
	// setting for all namespaces. However we want namespaces to honor the
	// journal.persistent setting, and this only works if Storage is set
	// to 'auto'.
	// See https://www.freedesktop.org/software/systemd/man/journald.conf.html#Storage=
	template := `# Journald configuration for snap quota group %[1]s
[Journal]
Storage=auto
`
	buf := bytes.Buffer{}
	fmt.Fprintf(&buf, template, grp.Name)
	fmt.Fprint(&buf, sizeOptions, rateOptions)
	return buf.Bytes()
}

func stopUserServices(cli *client.Client, inter Interacter, services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	failures, err := cli.ServicesStop(ctx, services)
	for _, f := range failures {
		inter.Notify(fmt.Sprintf("Could not stop service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func startUserServices(cli *client.Client, inter Interacter, services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	startFailures, stopFailures, err := cli.ServicesStart(ctx, services)
	for _, f := range startFailures {
		inter.Notify(fmt.Sprintf("Could not start service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	for _, f := range stopFailures {
		inter.Notify(fmt.Sprintf("While trying to stop previously started service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func stopService(sysd systemd.Systemd, inter Interacter, scope snap.DaemonScope, svcs []string) error {
	switch scope {
	case snap.SystemDaemon:
		if err := sysd.Stop(svcs); err != nil {
			return err
		}

	case snap.UserDaemon:
		cli := client.New()
		if err := stopUserServices(cli, inter, svcs...); err != nil {
			return err
		}
	default:
		panic("unknown app.DaemonScope")
	}

	return nil
}

func serviceIsActivated(app *snap.AppInfo) bool {
	return len(app.Sockets) > 0 || app.Timer != nil || len(app.ActivatesOn) > 0
}

func serviceIsSlotActivated(app *snap.AppInfo) bool {
	return len(app.ActivatesOn) > 0
}

// serviceUnits returns the service unit of the primary service, and a list
// of service units for the activation services.
func serviceUnits(app *snap.AppInfo) (service string, activators []string) {
	// Add application sockets
	for _, socket := range app.Sockets {
		activators = append(activators, filepath.Base(socket.File()))
	}
	// Sort the results from sockets for consistency
	sort.Strings(activators)

	// Add application timer
	if app.Timer != nil {
		activators = append(activators, filepath.Base(app.Timer.File()))
	}
	return app.ServiceName(), activators
}

// StartServicesFlags carries extra flags for StartServices.
type StartServicesFlags struct {
	Enable bool
}

// StartServices starts service units for the applications from the snap which
// are services. Service units will be started in the order provided by the
// caller.
func StartServices(apps []*snap.AppInfo, disabledSvcs []string, flags *StartServicesFlags, inter Interacter, tm timings.Measurer) (err error) {
	if flags == nil {
		flags = &StartServicesFlags{}
	}

	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)
	cli := client.New()

	var toEnableSystem []string
	var toEnableUser []string
	systemServices := make([]string, 0, len(apps))
	userServices := make([]string, 0, len(apps))
	servicesStarted := false

	defer func() {
		if err == nil {
			return
		}
		// apps could have been sorted according to their startup
		// ordering, stop them in reverse order
		if servicesStarted {
			for i := len(apps) - 1; i >= 0; i-- {
				app := apps[i]
				svc, activators := serviceUnits(app)
				if e := stopService(systemSysd, inter, app.DaemonScope, append(activators, svc)); e != nil {
					inter.Notify(fmt.Sprintf("While trying to stop previously started service %q: %v", app.ServiceName(), e))
				}
			}
		}
		if len(toEnableSystem) > 0 {
			if e := systemSysd.DisableNoReload(toEnableSystem); e != nil {
				inter.Notify(fmt.Sprintf("While trying to disable previously enabled services %q: %v", toEnableSystem, e))
			}
			if e := systemSysd.DaemonReload(); e != nil {
				inter.Notify(fmt.Sprintf("While trying to do daemon-reload: %v", e))
			}
		}
		if len(toEnableUser) > 0 {
			if e := userSysd.DisableNoReload(toEnableUser); e != nil {
				inter.Notify(fmt.Sprintf("while trying to disable previously enabled user services %q: %v", toEnableUser, e))
			}
		}
	}()
	// process all services of the snap in the order specified by the
	// caller; before batched calls were introduced, the sockets and timers
	// were started first, followed by other non-activated services
	markServicesForStart := func(svcs []string, scope snap.DaemonScope) {
		switch scope {
		case snap.SystemDaemon:
			systemServices = append(systemServices, svcs...)
		case snap.UserDaemon:
			userServices = append(userServices, svcs...)
		}
	}
	markServicesForEnable := func(svcs []string, scope snap.DaemonScope) {
		switch scope {
		case snap.SystemDaemon:
			toEnableSystem = append(toEnableSystem, svcs...)
		case snap.UserDaemon:
			toEnableUser = append(toEnableUser, svcs...)
		}
	}
	// first, gather all socket and timer units
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		if strutil.ListContains(disabledSvcs, app.Name) {
			continue
		}
		// Get all units for the service, but we only deal with
		// the activators here.
		_, activators := serviceUnits(app)
		if len(activators) == 0 {
			// just skip if there are no activated units
			continue
		}
		markServicesForStart(activators, app.DaemonScope)
		if flags.Enable {
			markServicesForEnable(activators, app.DaemonScope)
		}
	}

	// now collect all services
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		if serviceIsActivated(app) {
			continue
		}
		if strutil.ListContains(disabledSvcs, app.Name) {
			continue
		}
		svcName := app.ServiceName()
		markServicesForStart([]string{svcName}, app.DaemonScope)
		if flags.Enable {
			markServicesForEnable([]string{svcName}, app.DaemonScope)
		}
	}

	timings.Run(tm, "enable-services", fmt.Sprintf("enable services %q", toEnableSystem), func(nested timings.Measurer) {
		if len(toEnableSystem) > 0 {
			if err = systemSysd.EnableNoReload(toEnableSystem); err != nil {
				return
			}
			if err = systemSysd.DaemonReload(); err != nil {
				return
			}
		}
		if len(toEnableUser) > 0 {
			err = userSysd.EnableNoReload(toEnableUser)
		}
	})
	if err != nil {
		return err
	}

	timings.Run(tm, "start-services", "start services", func(nestedTm timings.Measurer) {
		for _, srv := range systemServices {
			// let the cleanup know some services may have been started
			servicesStarted = true
			// starting all services at once does not create a
			// single transaction, but instead spawns multiple jobs,
			// make sure the services started in the original order
			// by bringing them up one by one, see:
			// https://github.com/systemd/systemd/issues/8102
			// https://lists.freedesktop.org/archives/systemd-devel/2018-January/040152.html
			timings.Run(nestedTm, "start-service", fmt.Sprintf("start service %q", srv), func(_ timings.Measurer) {
				err = systemSysd.Start([]string{srv})
			})
			if err != nil {
				return
			}
		}
	})
	if servicesStarted && err != nil {
		// cleanup is handled in a defer
		return err
	}

	if len(userServices) != 0 {
		timings.Run(tm, "start-user-services", "start user services", func(nested timings.Measurer) {
			err = startUserServices(cli, inter, userServices...)
		})
		// let the cleanup know some services may have been started
		servicesStarted = true
		if err != nil {
			return err
		}
	}

	return nil
}

func userDaemonReload() error {
	cli := client.New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	return cli.ServicesDaemonReload(ctx)
}

func tryFileUpdate(path string, desiredContent []byte) (old *osutil.MemoryFileState, modified bool, err error) {
	newFileState := osutil.MemoryFileState{
		Content: desiredContent,
		Mode:    os.FileMode(0644),
	}

	// get the existing content (if any) of the file to have something to
	// rollback to if we have any errors

	// note we can't use FileReference here since we may be modifying
	// the file, and the FileReference.State() wouldn't be evaluated
	// until _after_ we attempted modification
	oldFileState := osutil.MemoryFileState{}

	if st, err := os.Stat(path); err == nil {
		b, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, false, err
		}
		oldFileState.Content = b
		oldFileState.Mode = st.Mode()
		newFileState.Mode = st.Mode()

		// save the old state of the file
		old = &oldFileState
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0755); mkdirErr != nil {
		return nil, false, mkdirErr
	}
	ensureErr := osutil.EnsureFileState(path, &newFileState)
	switch ensureErr {
	case osutil.ErrSameState:
		// didn't change the file
		return old, false, nil
	case nil:
		// we successfully modified the file
		return old, true, nil
	default:
		// some other fatal error trying to write the file
		return nil, false, ensureErr
	}
}

type SnapServiceOptions struct {
	// VitalityRank is the rank of all services in the specified snap used by
	// the OOM killer when OOM conditions are reached.
	VitalityRank int

	// QuotaGroup is the quota group for the specified snap.
	QuotaGroup *quota.Group
}

// ObserveChangeCallback can be invoked by EnsureSnapServices to observe
// the previous content of a unit and the new on a change.
// unitType can be "service", "socket", "timer". name is empty for a timer.
type ObserveChangeCallback func(app *snap.AppInfo, grp *quota.Group, unitType string, name, old, new string)

// EnsureSnapServicesOptions is the set of options applying to the
// EnsureSnapServices operation. It does not include per-snap specific options
// such as VitalityRank or RequireMountedSnapdSnap from AddSnapServiceOptions,
// since those are expected to be provided in the snaps argument.
type EnsureSnapServicesOptions struct {
	// Preseeding is whether the system is currently being preseeded, in which
	// case there is not a running systemd for EnsureSnapServicesOptions to
	// issue commands like systemctl daemon-reload to.
	Preseeding bool

	// RequireMountedSnapdSnap is whether the generated units should depend on
	// the snapd snap being mounted, this is specific to systems like UC18 and
	// UC20 which have the snapd snap and need to have units generated
	RequireMountedSnapdSnap bool

	// IncludeServices provides the option for allowing filtering which services
	// that should be configured. The service list must be in the format my-snap.my-service.
	// If this list is not provided, then all services will be included.
	IncludeServices []string
}

// ensureSnapServicesContext is the context for EnsureSnapServices.
// EnsureSnapServices supports transactional update/write of systemd service
// files and slice files. A part of this is to support rollback of files and
// also keep track of whether a restart of systemd daemon is required.
type ensureSnapServicesContext struct {
	// snaps, observeChange, opts and inter are the arguments
	// taken by EnsureSnapServices. They are here to allow sub-functions
	// to easier access these, and keep parameter lists shorter.
	snaps         map[*snap.Info]*SnapServiceOptions
	observeChange ObserveChangeCallback
	opts          *EnsureSnapServicesOptions
	inter         Interacter

	// note: is not used when preseeding is set in opts.Preseeding
	sysd                     systemd.Systemd
	systemDaemonReloadNeeded bool
	userDaemonReloadNeeded   bool
	// modifiedUnits is the set of units that were modified and the previous
	// state of the unit before modification that we can roll back to if there
	// are any issues.
	// note that the rollback is best effort, if we are rebooted in the middle,
	// there is no guarantee about the state of files, some may have been
	// updated and some may have been rolled back, higher level tasks/changes
	// should have do/undo handlers to properly handle the case where this
	// function is interrupted midway
	modifiedUnits map[string]*osutil.MemoryFileState
}

// restore is a helper function which should be called in case any errors happen
// during the write/update of systemd files
func (es *ensureSnapServicesContext) restore() {
	for file, state := range es.modifiedUnits {
		if state == nil {
			// we don't have anything to rollback to, so just remove the
			// file
			if err := os.Remove(file); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to remove %s due to previous failure: %v", file, err))
			}
		} else {
			// rollback the file to the previous state
			if err := osutil.EnsureFileState(file, state); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to rollback %s due to previous failure: %v", file, err))
			}
		}
	}

	if !es.opts.Preseeding {
		if es.systemDaemonReloadNeeded {
			if err := es.sysd.DaemonReload(); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to perform systemd daemon-reload due to previous failure: %v", err))
			}
		}
		if es.userDaemonReloadNeeded {
			if err := userDaemonReload(); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to perform user systemd daemon-reload due to previous failure: %v", err))
			}
		}
	}
}

// reloadModified uses the modifiedSystemServices/userDaemonReloadNeeded to determine whether a reload
// is required from systemd to take the new systemd files into effect. This is a NOP
// if opts.Preseeding is set
func (es *ensureSnapServicesContext) reloadModified() error {
	if es.opts.Preseeding {
		return nil
	}

	if es.systemDaemonReloadNeeded {
		if err := es.sysd.DaemonReload(); err != nil {
			return err
		}
	}
	if es.userDaemonReloadNeeded {
		if err := userDaemonReload(); err != nil {
			return err
		}
	}
	return nil
}

// ensureSnapServiceSystemdUnits takes care of writing .service files for all services
// registered in snap.Info apps.
func (es *ensureSnapServicesContext) ensureSnapServiceSystemdUnits(snapInfo *snap.Info, opts *generateSnapServicesOptions) error {
	handleFileModification := func(app *snap.AppInfo, unitType string, name, path string, content []byte) error {
		old, modifiedFile, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if modifiedFile {
			if es.observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				es.observeChange(app, nil, unitType, name, string(oldContent), string(content))
			}
			es.modifiedUnits[path] = old

			// also mark that we need to reload either the system or
			// user instance of systemd
			switch app.DaemonScope {
			case snap.SystemDaemon:
				es.systemDaemonReloadNeeded = true
			case snap.UserDaemon:
				es.userDaemonReloadNeeded = true
			}
		}

		return nil
	}

	// lets sort the service list before generating them for
	// consistency when testing
	services := snapInfo.Services()
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	var svcQuotaMap map[string]*quota.Group
	if opts.QuotaGroup != nil {
		svcQuotaMap = opts.QuotaGroup.ServiceMap()
	}

	// note that the Preseeding option is not used here at all
	for _, svc := range services {
		// if an inclusion list is provided, then we want to make sure this service
		// is included.
		// TODO: add an AppInfo.FullName member
		fullServiceName := fmt.Sprintf("%s.%s", snapInfo.InstanceName(), svc.Name)
		if len(es.opts.IncludeServices) > 0 && !strutil.ListContains(es.opts.IncludeServices, fullServiceName) {
			continue
		}

		// create services first; this doesn't trigger systemd

		// get the correct quota group for the service we are generating.
		quotaGrp := opts.QuotaGroup
		if quotaGrp != nil {
			// the service map contains quota group overrides if the service
			// entry exists.
			quotaGrp = svcQuotaMap[fullServiceName]
			if quotaGrp == nil {
				// default to the parent quota group, which may also be nil.
				quotaGrp = opts.QuotaGroup
			}
		}

		// Generate new service file state, make an app-specific generateSnapServicesOptions
		// to avoid modifying the original copy, if we were to override the quota group.
		content, err := generateSnapServiceFile(svc, &generateSnapServicesOptions{
			QuotaGroup:              quotaGrp,
			VitalityRank:            opts.VitalityRank,
			RequireMountedSnapdSnap: opts.RequireMountedSnapdSnap,
		})
		if err != nil {
			return err
		}

		path := svc.ServiceFile()
		if err := handleFileModification(svc, "service", svc.Name, path, content); err != nil {
			return err
		}

		// Generate systemd .socket files if needed
		socketFiles, err := generateSnapSocketFiles(svc)
		if err != nil {
			return err
		}
		for name, content := range socketFiles {
			path := svc.Sockets[name].File()
			if err := handleFileModification(svc, "socket", name, path, content); err != nil {
				return err
			}
		}

		if svc.Timer != nil {
			content, err := generateSnapTimerFile(svc)
			if err != nil {
				return err
			}
			path := svc.Timer.File()
			if err := handleFileModification(svc, "timer", "", path, content); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureSnapsSystemdServices takes care of writing .service files for all apps in the provided snaps
// list, and also returns a quota group set that represents all quota groups for the set of snaps
// provided if they are a part of any.
func (es *ensureSnapServicesContext) ensureSnapsSystemdServices() (*quota.QuotaGroupSet, error) {
	neededQuotaGrps := &quota.QuotaGroupSet{}

	for s, snapSvcOpts := range es.snaps {
		if s.Type() == snap.TypeSnapd {
			return nil, fmt.Errorf("internal error: adding explicit services for snapd snap is unexpected")
		}
		if snapSvcOpts == nil {
			snapSvcOpts = &SnapServiceOptions{}
		}

		// always use RequireMountedSnapdSnap options from the global options
		genServiceOpts := &generateSnapServicesOptions{
			RequireMountedSnapdSnap: es.opts.RequireMountedSnapdSnap,
			VitalityRank:            snapSvcOpts.VitalityRank,
			QuotaGroup:              snapSvcOpts.QuotaGroup,
		}
		if snapSvcOpts.QuotaGroup != nil {
			// AddAllNecessaryGroups also adds all sub-groups to the quota group set. So this
			// automatically covers any other quota group that might be set in snapSvcOpts.ServiceQuotaMap
			if err := neededQuotaGrps.AddAllNecessaryGroups(snapSvcOpts.QuotaGroup); err != nil {
				// this error can basically only be a circular reference
				// in the quota group tree
				return nil, err
			}
		}

		if err := es.ensureSnapServiceSystemdUnits(s, genServiceOpts); err != nil {
			return nil, err
		}
	}
	return neededQuotaGrps, nil
}

func (es *ensureSnapServicesContext) ensureSnapSlices(quotaGroups *quota.QuotaGroupSet) error {
	handleSliceModification := func(grp *quota.Group, path string, content []byte) error {
		old, modifiedFile, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if modifiedFile {
			if es.observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				es.observeChange(nil, grp, "slice", grp.Name, string(oldContent), string(content))
			}

			es.modifiedUnits[path] = old

			// also mark that we need to reload the system instance of systemd
			// TODO: also handle reloading the user instance of systemd when
			// needed
			es.systemDaemonReloadNeeded = true
		}

		return nil
	}

	// now make sure that all of the slice units exist
	for _, grp := range quotaGroups.AllQuotaGroups() {
		content := generateGroupSliceFile(grp)

		sliceFileName := grp.SliceFileName()
		path := filepath.Join(dirs.SnapServicesDir, sliceFileName)
		if err := handleSliceModification(grp, path, content); err != nil {
			return err
		}
	}
	return nil
}

func (es *ensureSnapServicesContext) ensureSnapJournaldUnits(quotaGroups *quota.QuotaGroupSet) error {
	handleJournalModification := func(grp *quota.Group, path string, content []byte) error {
		old, fileModified, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if !fileModified {
			return nil
		}

		// suppress any event and restart if we actually did not do anything
		// as it seems modifiedFile is set even when the file does not exist
		// and when the new content is nil.
		if (old == nil || len(old.Content) == 0) && len(content) == 0 {
			return nil
		}

		if es.observeChange != nil {
			var oldContent []byte
			if old != nil {
				oldContent = old.Content
			}
			es.observeChange(nil, grp, "journald", grp.Name, string(oldContent), string(content))
		}

		es.modifiedUnits[path] = old
		return nil
	}

	for _, grp := range quotaGroups.AllQuotaGroups() {
		if len(grp.Services) > 0 {
			// ignore service sub-groups
			continue
		}

		contents := generateJournaldConfFile(grp)
		fileName := grp.JournalConfFileName()

		path := filepath.Join(dirs.SnapSystemdDir, fileName)
		if err := handleJournalModification(grp, path, contents); err != nil {
			return err
		}
	}
	return nil
}

// ensureJournalQuotaServiceUnits takes care of writing service drop-in files for all journal namespaces.
func (es *ensureSnapServicesContext) ensureJournalQuotaServiceUnits(quotaGroups *quota.QuotaGroupSet) error {
	handleFileModification := func(grp *quota.Group, path string, content []byte) error {
		old, fileModified, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if fileModified {
			if es.observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				es.observeChange(nil, grp, "service", grp.Name, string(oldContent), string(content))
			}
			es.modifiedUnits[path] = old
		}
		return nil
	}

	for _, grp := range quotaGroups.AllQuotaGroups() {
		if grp.JournalLimit == nil {
			continue
		}

		if err := os.MkdirAll(grp.JournalServiceDropInDir(), 0755); err != nil {
			return err
		}

		dropInPath := grp.JournalServiceDropInFile()
		content := genJournalServiceFile(grp)
		if err := handleFileModification(grp, dropInPath, content); err != nil {
			return err
		}
	}

	return nil
}

// EnsureSnapServices will ensure that the specified snap services' file states
// are up to date with the specified options and infos. It will add new services
// if those units don't already exist, but it does not delete existing service
// units that are not present in the snap's Info structures.
// There are two sets of options; there are global options which apply to the
// entire transaction and to every snap service that is ensured, and options
// which are per-snap service and specified in the map argument.
// If any errors are encountered trying to update systemd units, then all
// changes performed up to that point are rolled back, meaning newly written
// units are deleted and modified units are attempted to be restored to their
// previous state.
// To observe which units were added or modified a
// ObserveChangeCallback calllback can be provided. The callback is
// invoked while processing the changes. Because of that it should not
// produce immediate side-effects, as the changes are in effect only
// if the function did not return an error.
// This function is idempotent.
func EnsureSnapServices(snaps map[*snap.Info]*SnapServiceOptions, opts *EnsureSnapServicesOptions, observeChange ObserveChangeCallback, inter Interacter) (err error) {
	if opts == nil {
		opts = &EnsureSnapServicesOptions{}
	}

	context := &ensureSnapServicesContext{
		snaps:         snaps,
		observeChange: observeChange,
		opts:          opts,
		inter:         inter,
		sysd:          systemd.New(systemd.SystemMode, inter),
		modifiedUnits: make(map[string]*osutil.MemoryFileState),
	}

	defer func() {
		if err == nil {
			return
		}
		context.restore()
	}()

	quotaGroups, err := context.ensureSnapsSystemdServices()
	if err != nil {
		return err
	}

	if err := context.ensureSnapSlices(quotaGroups); err != nil {
		return err
	}

	if err := context.ensureSnapJournaldUnits(quotaGroups); err != nil {
		return err
	}

	if err := context.ensureJournalQuotaServiceUnits(quotaGroups); err != nil {
		return err
	}

	return context.reloadModified()
}

// generateSnapServicesOptions is a struct for controlling the generated service
// definition for a snap service.
type generateSnapServicesOptions struct {
	// VitalityRank is the rank of all services in the specified snap used by
	// the OOM killer when OOM conditions are reached.
	VitalityRank int

	// QuotaGroup is the quota group for the service.
	QuotaGroup *quota.Group

	// RequireMountedSnapdSnap is whether the generated units should depend on
	// the snapd snap being mounted, this is specific to systems like UC18 and
	// UC20 which have the snapd snap and need to have units generated
	RequireMountedSnapdSnap bool
}

// StopServicesFlags carries extra flags for StopServices.
type StopServicesFlags struct {
	Disable bool
}

// StopServices stops and optionally disables service units for the applications
// from the snap which are services.
func StopServices(apps []*snap.AppInfo, flags *StopServicesFlags, reason snap.ServiceStopReason, inter Interacter, tm timings.Measurer) error {
	sysd := systemd.New(systemd.SystemMode, inter)
	if flags == nil {
		flags = &StopServicesFlags{}
	}

	if reason != snap.StopReasonOther {
		logger.Debugf("StopServices called for %q, reason: %v", apps, reason)
	} else {
		logger.Debugf("StopServices called for %q", apps)
	}

	disableServices := []string{}
	for _, app := range apps {
		// Handle the case where service file doesn't exist and don't try to stop it as it will fail.
		// This can happen with snap try when snap.yaml is modified on the fly and a daemon line is added.
		if !app.IsService() || !osutil.FileExists(app.ServiceFile()) {
			continue
		}
		// Skip stop on refresh when refresh mode is set to something
		// other than "restart" (or "" which is the same)
		if reason == snap.StopReasonRefresh {
			logger.Debugf(" %s refresh-mode: %v", app.Name, app.StopMode)
			switch app.RefreshMode {
			case "endure":
				// skip this service
				continue
			}
		}

		// Is the service slot activated, then lets warn the user this doesn't have any
		// real effect if a disable was requested
		if flags.Disable && serviceIsSlotActivated(app) {
			logger.Noticef("Disabling %s may not have the intended effect as the service is currently always activated by a slot", app.Name)
		}

		// Get services including any activation mechanisms. When stopping and disabling
		// services we do it on both the primary service, and it's activation mechanisms. The
		// StartServices logic does actually not enable/start any service which are activated,
		// but rather only the activation services themselves, so one might argue if it
		// is really necessary to disable the primary service.
		svc, activators := serviceUnits(app)

		var err error
		timings.Run(tm, "stop-service", fmt.Sprintf("stop service %q", app.ServiceName()), func(nested timings.Measurer) {
			err = stopService(sysd, inter, app.DaemonScope, append(activators, svc))
			if err == nil && flags.Disable {
				disableServices = append(disableServices, append(activators, svc)...)
			}
		})
		if err != nil {
			return err
		}
	}

	if len(disableServices) > 0 {
		if err := sysd.DisableNoReload(disableServices); err != nil {
			return err
		}
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveQuotaGroup ensures that the slice file for a quota group is removed. It
// assumes that the slice corresponding to the group is not in use anymore by
// any services or sub-groups of the group when it is invoked. To remove a group
// with sub-groups, one must remove all the sub-groups first.
// This function is idempotent, if the slice file doesn't exist no error is
// returned.
func RemoveQuotaGroup(grp *quota.Group, inter Interacter) error {
	// TODO: it only works on leaf sub-groups currently
	if len(grp.SubGroups) != 0 {
		return fmt.Errorf("internal error: cannot remove quota group with sub-groups")
	}

	systemSysd := systemd.New(systemd.SystemMode, inter)

	// remove the slice file
	err := os.Remove(filepath.Join(dirs.SnapServicesDir, grp.SliceFileName()))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err == nil {
		// we deleted the slice unit, so we need to daemon-reload
		if err := systemSysd.DaemonReload(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveSnapServices disables and removes service units for the applications
// from the snap which are services. The optional flag indicates whether
// services are removed as part of undoing of first install of a given snap.
func RemoveSnapServices(s *snap.Info, inter Interacter) error {
	if s.Type() == snap.TypeSnapd {
		return fmt.Errorf("internal error: removing explicit services for snapd snap is unexpected")
	}
	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)
	var removedSystem, removedUser bool
	systemUnits := []string{}
	userUnits := []string{}
	systemUnitFiles := []string{}

	// collect list of system units to disable and remove
	for _, app := range s.Apps {
		if !app.IsService() || !osutil.FileExists(app.ServiceFile()) {
			continue
		}

		switch app.DaemonScope {
		case snap.SystemDaemon:
			removedSystem = true
		case snap.UserDaemon:
			removedUser = true
		}
		serviceName := filepath.Base(app.ServiceFile())

		for _, socket := range app.Sockets {
			path := socket.File()
			socketServiceName := filepath.Base(path)
			logger.Noticef("RemoveSnapServices - socket %s", socketServiceName)
			switch app.DaemonScope {
			case snap.SystemDaemon:
				systemUnits = append(systemUnits, socketServiceName)
			case snap.UserDaemon:
				userUnits = append(userUnits, socketServiceName)
			}
			systemUnitFiles = append(systemUnitFiles, path)
		}

		if app.Timer != nil {
			path := app.Timer.File()

			timerName := filepath.Base(path)
			logger.Noticef("RemoveSnapServices - timer %s", timerName)
			switch app.DaemonScope {
			case snap.SystemDaemon:
				systemUnits = append(systemUnits, timerName)
			case snap.UserDaemon:
				userUnits = append(userUnits, timerName)
			}
			systemUnitFiles = append(systemUnitFiles, path)
		}

		logger.Noticef("RemoveSnapServices - disabling %s", serviceName)
		switch app.DaemonScope {
		case snap.SystemDaemon:
			systemUnits = append(systemUnits, serviceName)
		case snap.UserDaemon:
			userUnits = append(userUnits, serviceName)
		}
		systemUnitFiles = append(systemUnitFiles, app.ServiceFile())
	}

	// disable all collected systemd units
	if err := systemSysd.DisableNoReload(systemUnits); err != nil {
		return err
	}

	// disable all collected user units
	if err := userSysd.DisableNoReload(userUnits); err != nil {
		return err
	}

	// remove unit filenames
	for _, systemUnitFile := range systemUnitFiles {
		if err := os.Remove(systemUnitFile); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove socket file %q: %v", systemUnitFile, err)
		}
	}

	// only reload if we actually had services
	if removedSystem {
		if err := systemSysd.DaemonReload(); err != nil {
			return err
		}
	}
	if removedUser {
		if err := userDaemonReload(); err != nil {
			return err
		}
	}

	return nil
}

func genServiceNames(snap *snap.Info, appNames []string) []string {
	names := make([]string, 0, len(appNames))

	for _, name := range appNames {
		if app := snap.Apps[name]; app != nil {
			names = append(names, app.ServiceName())
		}
	}
	return names
}

func genServiceFile(appInfo *snap.AppInfo, opts *generateSnapServicesOptions) ([]byte, error) {
	if opts == nil {
		opts = &generateSnapServicesOptions{}
	}

	// assemble all of the service directive snippets for all interfaces that
	// this service needs to include in the generated systemd file

	// use an ordered set to ensure we don't duplicate any keys from interfaces
	// that specify the same snippet

	// TODO: maybe we should error if multiple interfaces specify different
	// values for the same directive, otherwise one of them will overwrite the
	// other? What happens right now is that the snippet from the plug that
	// comes last will win in the case of directives that can have only one
	// value, but for some directives, systemd combines their values into a
	// list.
	ifaceServiceSnippets := &strutil.OrderedSet{}

	for _, plug := range appInfo.Plugs {
		iface, err := interfaces.ByName(plug.Interface)
		if err != nil {
			return nil, fmt.Errorf("error processing plugs while generating service unit for %v: %v", appInfo.SecurityTag(), err)
		}
		snips, err := interfaces.PermanentPlugServiceSnippets(iface, plug)
		if err != nil {
			return nil, fmt.Errorf("error processing plugs while generating service unit for %v: %v", appInfo.SecurityTag(), err)
		}
		for _, snip := range snips {
			ifaceServiceSnippets.Put(snip)
		}
	}

	// join the service snippets into one string to be included in the
	// template
	ifaceSpecifiedServiceSnippet := strings.Join(ifaceServiceSnippets.Items(), "\n")

	serviceTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit }}
Requires={{.MountUnit}}
{{- end }}
{{- if .PrerequisiteTarget}}
Wants={{.PrerequisiteTarget}}
{{- end}}
{{- if .After}}
After={{ stringsJoin .After " " }}
{{- end}}
{{- if .Before}}
Before={{ stringsJoin .Before " "}}
{{- end}}
{{- if .CoreMountedSnapdSnapDep}}
Wants={{ stringsJoin .CoreMountedSnapdSnapDep " "}}
After={{ stringsJoin .CoreMountedSnapdSnapDep " "}}
{{- end}}
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart={{.App.LauncherCommand}}
SyslogIdentifier={{.App.Snap.InstanceName}}.{{.App.Name}}
Restart={{.Restart}}
{{- if .App.RestartDelay}}
RestartSec={{.App.RestartDelay.Seconds}}
{{- end}}
WorkingDirectory={{.WorkingDir}}
{{- if .App.StopCommand}}
ExecStop={{.App.LauncherStopCommand}}
{{- end}}
{{- if .App.ReloadCommand}}
ExecReload={{.App.LauncherReloadCommand}}
{{- end}}
{{- if .App.PostStopCommand}}
ExecStopPost={{.App.LauncherPostStopCommand}}
{{- end}}
{{- if .StopTimeout}}
TimeoutStopSec={{.StopTimeout.Seconds}}
{{- end}}
{{- if .StartTimeout}}
TimeoutStartSec={{.StartTimeout.Seconds}}
{{- end}}
Type={{.App.Daemon}}
{{- if .Remain}}
RemainAfterExit={{.Remain}}
{{- end}}
{{- if .BusName}}
BusName={{.BusName}}
{{- end}}
{{- if .App.WatchdogTimeout}}
WatchdogSec={{.App.WatchdogTimeout.Seconds}}
{{- end}}
{{- if .KillMode}}
KillMode={{.KillMode}}
{{- end}}
{{- if .KillSignal}}
KillSignal={{.KillSignal}}
{{- end}}
{{- if .OOMAdjustScore }}
OOMScoreAdjust={{.OOMAdjustScore}}
{{- end}}
{{- if .InterfaceServiceSnippets}}
{{.InterfaceServiceSnippets}}
{{- end}}
{{- if .SliceUnit}}
Slice={{.SliceUnit}}
{{- end}}
{{- if .LogNamespace}}
LogNamespace={{.LogNamespace}}
{{- end}}
{{- if not (or .App.Sockets .App.Timer .App.ActivatesOn) }}

[Install]
WantedBy={{.ServicesTarget}}
{{- end}}
`
	var templateOut bytes.Buffer
	tmpl := template.New("service-wrapper")
	tmpl.Funcs(template.FuncMap{
		"stringsJoin": strings.Join,
	})
	t := template.Must(tmpl.Parse(serviceTemplate))

	restartCond := appInfo.RestartCond.String()
	if restartCond == "" {
		restartCond = snap.RestartOnFailure.String()
	}

	// use score -900+vitalityRank, where vitalityRank starts at 1
	// and considering snapd itself has OOMScoreAdjust=-900
	const baseOOMAdjustScore = -900
	var oomAdjustScore int
	if opts.VitalityRank > 0 {
		oomAdjustScore = baseOOMAdjustScore + opts.VitalityRank
	}

	var remain string
	if appInfo.Daemon == "oneshot" {
		// any restart condition other than "no" is invalid for oneshot daemons
		restartCond = "no"
		// If StopExec is present for a oneshot service than we also need
		// RemainAfterExit=yes
		if appInfo.StopCommand != "" {
			remain = "yes"
		}
	}
	var killMode string
	if !appInfo.StopMode.KillAll() {
		killMode = "process"
	}

	var busName string
	if appInfo.Daemon == "dbus" {
		busName = appInfo.BusName
		if busName == "" && len(appInfo.ActivatesOn) != 0 {
			slot := appInfo.ActivatesOn[len(appInfo.ActivatesOn)-1]
			if err := slot.Attr("name", &busName); err != nil {
				// This should be impossible for a valid AppInfo
				logger.Noticef("Cannot get 'name' attribute of dbus slot %q: %v", slot.Name, err)
			}
		}
	}

	wrapperData := struct {
		App *snap.AppInfo

		Restart                  string
		WorkingDir               string
		StopTimeout              time.Duration
		StartTimeout             time.Duration
		ServicesTarget           string
		PrerequisiteTarget       string
		MountUnit                string
		Remain                   string
		KillMode                 string
		KillSignal               string
		OOMAdjustScore           int
		BusName                  string
		Before                   []string
		After                    []string
		InterfaceServiceSnippets string
		SliceUnit                string
		LogNamespace             string

		Home    string
		EnvVars string

		CoreMountedSnapdSnapDep []string
	}{
		App: appInfo,

		InterfaceServiceSnippets: ifaceSpecifiedServiceSnippet,

		Restart:        restartCond,
		StopTimeout:    serviceStopTimeout(appInfo),
		StartTimeout:   time.Duration(appInfo.StartTimeout),
		Remain:         remain,
		KillMode:       killMode,
		KillSignal:     appInfo.StopMode.KillSignal(),
		OOMAdjustScore: oomAdjustScore,
		BusName:        busName,

		Before: genServiceNames(appInfo.Snap, appInfo.Before),
		After:  genServiceNames(appInfo.Snap, appInfo.After),

		// systemd runs as PID 1 so %h will not work.
		Home: "/root",
	}
	switch appInfo.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.ServicesTarget = systemd.ServicesTarget
		wrapperData.PrerequisiteTarget = systemd.PrerequisiteTarget
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir()))
		wrapperData.WorkingDir = appInfo.Snap.DataDir()
		wrapperData.After = append(wrapperData.After, "snapd.apparmor.service")
	case snap.UserDaemon:
		wrapperData.ServicesTarget = systemd.UserServicesTarget
		// FIXME: ideally use UserDataDir("%h"), but then the
		// unit fails if the directory doesn't exist.
		wrapperData.WorkingDir = appInfo.Snap.DataDir()
	default:
		panic("unknown snap.DaemonScope")
	}

	// check the quota group slice
	if opts.QuotaGroup != nil {
		wrapperData.SliceUnit = opts.QuotaGroup.SliceFileName()
		if opts.QuotaGroup.JournalQuotaSet() {
			wrapperData.LogNamespace = opts.QuotaGroup.JournalNamespaceName()
		}
	}

	// Add extra "After" targets
	if wrapperData.PrerequisiteTarget != "" {
		wrapperData.After = append([]string{wrapperData.PrerequisiteTarget}, wrapperData.After...)
	}
	if wrapperData.MountUnit != "" {
		wrapperData.After = append([]string{wrapperData.MountUnit}, wrapperData.After...)
	}

	if opts.RequireMountedSnapdSnap {
		// on core 18+ systems, the snapd tooling is exported
		// into the host system via a special mount unit, which
		// also adds an implicit dependency on the snapd snap
		// mount thus /usr/bin/snap points
		wrapperData.CoreMountedSnapdSnapDep = []string{SnapdToolingMountUnit}
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}

func genServiceSocketFile(appInfo *snap.AppInfo, socketName string) []byte {
	socketTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket {{.SocketName}} for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit}}
Requires={{.MountUnit}}
After={{.MountUnit}}
{{- end}}
X-Snappy=yes

[Socket]
Service={{.ServiceFileName}}
FileDescriptorName={{.SocketInfo.Name}}
ListenStream={{.ListenStream}}
{{- if .SocketInfo.SocketMode}}
SocketMode={{.SocketInfo.SocketMode | printf "%04o"}}
{{- end}}

[Install]
WantedBy={{.SocketsTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("socket-wrapper").Parse(socketTemplate))

	socket := appInfo.Sockets[socketName]
	listenStream := renderListenStream(socket)
	wrapperData := struct {
		App             *snap.AppInfo
		ServiceFileName string
		SocketsTarget   string
		MountUnit       string
		SocketName      string
		SocketInfo      *snap.SocketInfo
		ListenStream    string
	}{
		App:             appInfo,
		ServiceFileName: filepath.Base(appInfo.ServiceFile()),
		SocketsTarget:   systemd.SocketsTarget,
		SocketName:      socketName,
		SocketInfo:      socket,
		ListenStream:    listenStream,
	}
	switch appInfo.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir()))
	case snap.UserDaemon:
		// nothing
	default:
		panic("unknown snap.DaemonScope")
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes()
}

func genJournalServiceFile(grp *quota.Group) []byte {
	buf := bytes.Buffer{}
	template := `[Service]
LogsDirectory=
`
	fmt.Fprint(&buf, template)
	return buf.Bytes()
}

func generateSnapSocketFiles(app *snap.AppInfo) (map[string][]byte, error) {
	if err := snap.ValidateApp(app); err != nil {
		return nil, err
	}

	socketFiles := make(map[string][]byte)
	for name := range app.Sockets {
		socketFiles[name] = genServiceSocketFile(app, name)
	}
	return socketFiles, nil
}

func renderListenStream(socket *snap.SocketInfo) string {
	s := socket.App.Snap
	listenStream := socket.ListenStream
	switch socket.App.DaemonScope {
	case snap.SystemDaemon:
		listenStream = strings.Replace(listenStream, "$SNAP_DATA", s.DataDir(), -1)
		// TODO: when we support User/Group in the generated
		// systemd unit, adjust this accordingly
		serviceUserUid := sys.UserID(0)
		runtimeDir := s.UserXdgRuntimeDir(serviceUserUid)
		listenStream = strings.Replace(listenStream, "$XDG_RUNTIME_DIR", runtimeDir, -1)
		listenStream = strings.Replace(listenStream, "$SNAP_COMMON", s.CommonDataDir(), -1)
	case snap.UserDaemon:
		// TODO: use SnapDirOpts here. User daemons are also an experimental
		// feature so, for simplicity, we can not pass opts here for now
		listenStream = strings.Replace(listenStream, "$SNAP_USER_DATA", s.UserDataDir("%h", nil), -1)
		listenStream = strings.Replace(listenStream, "$SNAP_USER_COMMON", s.UserCommonDataDir("%h", nil), -1)
		// FIXME: find some way to share code with snap.UserXdgRuntimeDir()
		listenStream = strings.Replace(listenStream, "$XDG_RUNTIME_DIR", fmt.Sprintf("%%t/snap.%s", s.InstanceName()), -1)
	default:
		panic("unknown snap.DaemonScope")
	}
	return listenStream
}

func generateSnapTimerFile(app *snap.AppInfo) ([]byte, error) {
	timerTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Timer {{.TimerName}} for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit}}
Requires={{.MountUnit}}
After={{.MountUnit}}
{{- end}}
X-Snappy=yes

[Timer]
Unit={{.ServiceFileName}}
{{ range .Schedules }}OnCalendar={{ . }}
{{ end }}
[Install]
WantedBy={{.TimersTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("timer-wrapper").Parse(timerTemplate))

	timerSchedule, err := timeutil.ParseSchedule(app.Timer.Timer)
	if err != nil {
		return nil, err
	}

	schedules := generateOnCalendarSchedules(timerSchedule)

	wrapperData := struct {
		App             *snap.AppInfo
		ServiceFileName string
		TimersTarget    string
		TimerName       string
		MountUnit       string
		Schedules       []string
	}{
		App:             app,
		ServiceFileName: filepath.Base(app.ServiceFile()),
		TimersTarget:    systemd.TimersTarget,
		TimerName:       app.Name,
		Schedules:       schedules,
	}
	switch app.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(app.Snap.MountDir()))
	case snap.UserDaemon:
		// nothing
	default:
		panic("unknown snap.DaemonScope")
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}

func makeAbbrevWeekdays(start time.Weekday, end time.Weekday) []string {
	out := make([]string, 0, 7)
	for w := start; w%7 != (end + 1); w++ {
		out = append(out, time.Weekday(w % 7).String()[0:3])
	}
	return out
}

// daysRange generates a string representing a continuous range between given
// day numbers, which due to compatiblilty with old systemd version uses a
// verbose syntax of x,y,z instead of x..z
func daysRange(start, end uint) string {
	var buf bytes.Buffer
	for i := start; i <= end; i++ {
		buf.WriteString(strconv.FormatInt(int64(i), 10))
		if i < end {
			buf.WriteRune(',')
		}
	}
	return buf.String()
}

// generateOnCalendarSchedules converts a schedule into OnCalendar schedules
// suitable for use in systemd *.timer units using systemd.time(7)
// https://www.freedesktop.org/software/systemd/man/systemd.time.html
// XXX: old systemd versions do not support x..y ranges
func generateOnCalendarSchedules(schedule []*timeutil.Schedule) []string {
	calendarEvents := make([]string, 0, len(schedule))
	for _, sched := range schedule {
		days := make([]string, 0, len(sched.WeekSpans))
		for _, week := range sched.WeekSpans {
			abbrev := strings.Join(makeAbbrevWeekdays(week.Start.Weekday, week.End.Weekday), ",")

			if week.Start.Pos == timeutil.EveryWeek && week.End.Pos == timeutil.EveryWeek {
				// eg: mon, mon-fri, fri-mon
				days = append(days, fmt.Sprintf("%s *-*-*", abbrev))
				continue
			}
			// examples:
			// mon1 - Mon *-*-1..7 (Monday during the first 7 days)
			// fri1 - Fri *-*-1..7 (Friday during the first 7 days)

			// entries below will make systemd timer expire more
			// frequently than the schedule suggests, however snap
			// runner evaluates current time and gates the actual
			// action
			//
			// mon1-tue - *-*-1..7 *-*-8 (anchored at first
			// Monday; Monday happens during the 7 days,
			// Tuesday can possibly happen on the 8th day if
			// the month started on Tuesday)
			//
			// mon-tue1 - *-*~1 *-*-1..7 (anchored at first
			// Tuesday; matching Monday can happen on the
			// last day of previous month if Tuesday is the
			// 1st)
			//
			// mon5-tue - *-*~1..7 *-*-1 (anchored at last
			// Monday, the matching Tuesday can still happen
			// within the last 7 days, or on the 1st of the
			// next month)
			//
			// fri4-mon - *-*-22-31 *-*-1..7 (anchored at 4th
			// Friday, can span onto the next month, extreme case in
			// February when 28th is Friday)
			//
			// XXX: since old versions of systemd, eg. 229 available
			// in 16.04 does not support x..y ranges, days need to
			// be enumerated like so:
			// Mon *-*-1..7 -> Mon *-*-1,2,3,4,5,6,7
			//
			// XXX: old systemd versions do not support the last n
			// days syntax eg, *-*~1, thus the range needs to be
			// generated in more verbose way like so:
			// Mon *-*~1..7 -> Mon *-*-22,23,24,25,26,27,28,29,30,31
			// (22-28 is the last week, but the month can have
			// anywhere from 28 to 31 days)
			//
			startPos := week.Start.Pos
			endPos := startPos
			if !week.AnchoredAtStart() {
				startPos = week.End.Pos
				endPos = startPos
			}
			startDay := (startPos-1)*7 + 1
			endDay := (endPos) * 7

			if week.IsSingleDay() {
				// single day, can use the 'weekday' filter
				if startPos == timeutil.LastWeek {
					// last week of a month, which can be
					// 22-28 in case of February, while
					// month can have between 28 and 31 days
					days = append(days,
						fmt.Sprintf("%s *-*-%s", abbrev, daysRange(22, 31)))
				} else {
					days = append(days,
						fmt.Sprintf("%s *-*-%s", abbrev, daysRange(startDay, endDay)))
				}
				continue
			}

			if week.AnchoredAtStart() {
				// explore the edge cases first
				switch startPos {
				case timeutil.LastWeek:
					// starts in the last week of the month and
					// possibly spans into the first week of the
					// next month;
					// month can have between 28 and 31
					// days
					days = append(days,
						// trailing 29-31 that are not part of a full week
						fmt.Sprintf("*-*-%s", daysRange(29, 31)),
						fmt.Sprintf("*-*-%s", daysRange(1, 7)))
				case 4:
					// a range in the 4th week can span onto
					// the next week, which is either 28-31
					// or in extreme case (eg. February with
					// 28 days) 1-7 of the next month
					days = append(days,
						// trailing 29-31 that are not part of a full week
						fmt.Sprintf("*-*-%s", daysRange(29, 31)),
						fmt.Sprintf("*-*-%s", daysRange(1, 7)))
				default:
					// can possibly spill into the next week
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay+7, endDay+7)))
				}

				if startDay < 28 {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay, endDay)))
				} else {
					// from the end of the month
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
			} else {
				switch endPos {
				case timeutil.LastWeek:
					// month can have between 28 and 31
					// days, add trailing 29-31 that are not
					// part of a full week
					days = append(days, fmt.Sprintf("*-*-%s", daysRange(29, 31)))
				case 1:
					// possibly spans from the last week of the
					// previous month and ends in the first week of
					// current month
					days = append(days, fmt.Sprintf("*-*-%s", daysRange(22, 31)))
				default:
					// can possibly spill into the previous week
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
				if endDay < 28 {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay, endDay)))
				} else {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
			}
		}

		if len(days) == 0 {
			// no weekday spec, meaning the timer runs every day
			days = []string{"*-*-*"}
		}

		startTimes := make([]string, 0, len(sched.ClockSpans))
		for _, clocks := range sched.ClockSpans {
			// use expanded clock spans
			for _, span := range clocks.ClockSpans() {
				when := span.Start
				if span.Spread {
					length := span.End.Sub(span.Start)
					if length < 0 {
						// span Start wraps around, so we have '00:00.Sub(23:45)'
						length = -length
					}
					if length > 5*time.Minute {
						// replicate what timeutil.Next() does
						// and cut some time at the end of the
						// window so that events do not happen
						// directly one after another
						length -= 5 * time.Minute
					}
					when = when.Add(randutil.RandomDuration(length))
				}
				if when.Hour == 24 {
					// 24:00 for us means the other end of
					// the day, for systemd we need to
					// adjust it to the 0-23 hour range
					when.Hour -= 24
				}

				startTimes = append(startTimes, when.String())
			}
		}

		for _, day := range days {
			if len(startTimes) == 0 {
				// current schedule is days only
				calendarEvents = append(calendarEvents, day)
				continue
			}

			for _, startTime := range startTimes {
				calendarEvents = append(calendarEvents, fmt.Sprintf("%s %s", day, startTime))
			}
		}
	}
	return calendarEvents
}

// serviceStatus represents the status of a service, and any of its activation
// service units. It also provides a method isEnabled which can determine the true
// enable status for services that are activated.
type serviceStatus struct {
	name        string
	service     *systemd.UnitStatus
	activators  []*systemd.UnitStatus
	slotEnabled bool
}

func (s *serviceStatus) isEnabled() bool {
	// If the service is slot activated, it cannot be disabled and thus always
	// is enabled.
	if s.slotEnabled {
		return true
	}

	// If there are no activator units, then return status of the
	// primary service.
	if len(s.activators) == 0 {
		return s.service.Enabled
	}

	// Just a single of those activators need to be enabled for us
	// to report the service as enabled.
	for _, a := range s.activators {
		if a.Enabled {
			return true
		}
	}
	return false
}

func appServiceUnitsMany(apps []*snap.AppInfo) []string {
	var allUnits []string
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		// TODO: handle user daemons
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}
		svc, activators := serviceUnits(app)
		allUnits = append(allUnits, svc)
		allUnits = append(allUnits, activators...)
	}
	return allUnits
}

func queryServiceStatusMany(sysd systemd.Systemd, apps []*snap.AppInfo) ([]*serviceStatus, error) {
	allUnits := appServiceUnitsMany(apps)
	unitStatuses, err := sysd.Status(allUnits)
	if err != nil {
		return nil, err
	}

	var appStatuses []*serviceStatus
	var statusIndex int
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		// TODO: handle user daemons
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}

		// This builds on the principle that sysd.Status returns service unit statuses
		// in the exact same order we requested them in.
		_, activators := serviceUnits(app)
		svcSt := &serviceStatus{
			name:        app.Name,
			service:     unitStatuses[statusIndex],
			slotEnabled: serviceIsSlotActivated(app),
		}
		if len(activators) > 0 {
			svcSt.activators = unitStatuses[statusIndex+1 : statusIndex+1+len(activators)]
		}
		appStatuses = append(appStatuses, svcSt)
		statusIndex += 1 + len(activators)
	}
	return appStatuses, nil
}

type RestartServicesFlags struct {
	// Reload set if we might need to reload the service definitions.
	Reload bool
	// AlsoEnabledNonActive set if we to restart also enabled but not running units
	AlsoEnabledNonActive bool
}

// Restart or reload active services in `svcs`.
// If reload flag is set then "systemctl reload-or-restart" is attempted.
// The services mentioned in `explicitServices` should be a subset of the
// services in svcs. The services included in explicitServices are always
// restarted, regardless of their state. The services in the `svcs` argument
// are only restarted if they are active, so if a service is meant to be
// restarted no matter it's state, it should be included in the
// explicitServices list.
// The list of explicitServices needs to use systemd unit names.
// TODO: change explicitServices format to be less unusual, more consistent
// (introduce AppRef?)
func RestartServices(apps []*snap.AppInfo, explicitServices []string,
	flags *RestartServicesFlags, inter Interacter, tm timings.Measurer) error {
	if flags == nil {
		flags = &RestartServicesFlags{}
	}
	sysd := systemd.New(systemd.SystemMode, inter)

	// Get service statuses for each of the apps
	sts, err := queryServiceStatusMany(sysd, apps)
	if err != nil {
		return err
	}

	for _, st := range sts {
		unitName := st.service.Name
		unitActive := st.service.Active
		unitEnabled := st.isEnabled()

		// If the unit was explicitly mentioned in the command line, restart it
		// even if it is disabled; otherwise, we only restart units which are
		// currently enabled or running. Reference:
		// https://forum.snapcraft.io/t/command-line-interface-to-manipulate-services/262/47
		if !unitActive && !strutil.ListContains(explicitServices, unitName) {
			if !flags.AlsoEnabledNonActive {
				logger.Noticef("not restarting inactive unit %s", unitName)
				continue
			} else if !unitEnabled {
				logger.Noticef("not restarting disabled and inactive unit %s", unitName)
				continue
			}
		}

		var err error
		timings.Run(tm, "restart-service", fmt.Sprintf("restart service %s", unitName), func(nested timings.Measurer) {
			if flags.Reload {
				err = sysd.ReloadOrRestart([]string{unitName})
			} else {
				// note: stop followed by start, not just 'restart'
				err = sysd.Restart([]string{unitName})
			}
		})
		if err != nil {
			// there is nothing we can do about failed service
			return err
		}
	}
	return nil
}

// ServicesEnableState returns a map of service names from the given snap,
// together with their enable/disable status.
func ServicesEnableState(s *snap.Info, inter Interacter) (map[string]bool, error) {
	sysd := systemd.New(systemd.SystemMode, inter)
	sts, err := queryServiceStatusMany(sysd, s.Services())
	if err != nil {
		return nil, err
	}

	// loop over all services in the snap, storing the current enable status
	snapSvcsState := make(map[string]bool, len(sts))
	for _, st := range sts {
		snapSvcsState[st.name] = st.isEnabled()
	}
	return snapSvcsState, nil
}

// QueryDisabledServices returns a list of all currently disabled snap services
// in the snap.
func QueryDisabledServices(info *snap.Info, pb progress.Meter) ([]string, error) {
	sysd := systemd.New(systemd.SystemMode, pb)
	sts, err := queryServiceStatusMany(sysd, info.Services())
	if err != nil {
		return nil, err
	}

	// add all disabled services to the list
	disabledSnapSvcs := []string{}
	for _, st := range sts {
		if !st.isEnabled() {
			disabledSnapSvcs = append(disabledSnapSvcs, st.name)
		}
	}

	// sort for easier testing
	sort.Strings(disabledSnapSvcs)

	return disabledSnapSvcs, nil
}
