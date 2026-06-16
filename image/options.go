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

package image

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
)

type Options struct {
	ModelFile string
	Classic   bool

	// Preseed requests the image to be preseeded (only for UC20)
	Preseed bool
	// PreseedSignKey is the name of the key to use for signing preseed
	// assertion (empty means the default key).
	PreseedSignKey string

	// AppArmor kernel features directory to bind-mount when preseeding.
	// If empty then the features from /sys/kernel/security/apparmor will be used.
	// (only for UC20)
	AppArmorKernelFeaturesDir string

	// SysfsOverlay is the optional sysfs overlay to be used for
	// preseeding.
	// Directories from /sys/class/* and /sys/devices/platform
	// will be bind-mounted to the chroot when preseeding.
	SysfsOverlay string

	Channel string

	// TODO: use OptionsSnap directly here?
	Snaps        []string
	Components   []string
	SnapChannels map[string]string

	// SeedManifest is a pre-provided seed manifest, to allow for
	// creating reproducible seeds. If provided, the snap revisions and
	// validation-sets specified in the seed manifest will be used to
	// (re-)create the image seed.
	SeedManifest *seedwriter.Manifest
	// SeedManifestPath if set, specifies the file path where the
	// seed.manifest file should be written.
	SeedManifestPath string

	// WideCohortKey can be used to supply a cohort covering all
	// the snaps in the image, there is no generally suppported API
	// to create such a cohort key.
	WideCohortKey string

	PrepareDir string

	// Architecture to use if none is specified by the model,
	// useful only for classic mode. If set must match the model otherwise.
	Architecture string

	// AllowSnapdKernelMismatch if set, will allow building images with a snap/kernel
	// combination that would otherwise be unsupported.
	AllowSnapdKernelMismatch bool

	Customizations Customizations

	// Assertion files to inject into the built image
	// The first field is for filenames passed as input, the second one
	// for the validated assertions that the Writer and Fetcher will use
	ExtraAssertionsFiles []string
	ExtraAssertions      []asserts.Assertion

	// AssertionRetrieve, when set, replaces the snap store as the
	// source of assertions during image build. This is the
	// transport hook used by offline image builds: a closed
	// ecosystem provides a function that resolves Refs from local
	// data instead of going over the network. The strict assertion
	// pipeline (prereq walk, save into seed) is unchanged.
	AssertionRetrieve func(ref *asserts.Ref) (asserts.Assertion, error)

	// SnapDownloadURL, when set, replaces the snap store's
	// metadata/URL-resolution step with a caller-provided lookup.
	// The hook returns a fully-formed HTTPS URL pointing at the
	// .snap blob for the given snap at the given revision; snapd
	// HTTP-GETs that URL, populates snap.Info from the downloaded
	// file, and runs the rest of the seed pipeline (snap-revision
	// sha3 match, assertion chain, seed writing) unchanged. The
	// resulting image is identical to one built via the normal
	// store path.
	//
	// Intended for closed-ecosystem appstores whose metadata API
	// is not snap-store-compatible (e.g. GraphQL-fronted stores)
	// but whose download endpoint is a plain signed HTTPS URL.
	SnapDownloadURL func(name string, revision snap.Revision, snapID string) (url string, err error)

	// AllowExtraSnaps, when set, lets the seed include snaps not
	// declared in the model (and override channels) regardless of the
	// model's grade -- behaviour otherwise restricted to grade
	// dangerous. Intended for trusted external builders that pin an
	// exact snap set in a manifest and verify the image out of band
	// (e.g. liot-image manifest mode). Affects image composition only;
	// the seeded snaps are installed by the device's own snapd at
	// boot, which does not consult this flag.
	AllowExtraSnaps bool
}

// Customizatons defines possible image customizations. Not all of
// them applies to all kind of systems.
type Customizations struct {
	// ConsoleConf can be set to "disabled" to disable console-conf
	// forcefully (UC16/18 only ATM).
	ConsoleConf string `json:"console-conf"`
	// CloudInitUserData can optionally point to cloud init user-data
	// (UC16/18 only)
	CloudInitUserData string `json:"cloud-init-user-data"`
	// BootFlags can be set to a list of boot flags
	// to set in the recovery bootloader (UC20 only).
	// Currently only the "factory" hint flag is supported.
	BootFlags []string `json:"boot-flags"`
	// Validation controls whether validations should be taken
	// into account by the store to select snap revisions.
	// It can be set to "enforce" or "ignore".
	Validation string `json:"validation"`
}
