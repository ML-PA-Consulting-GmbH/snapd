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

package builtin

import (
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const raucSupportSummary = `allows access to the RAUC update service via D-Bus`

const raucSupportBaseDeclarationSlots = `
  rauc-support:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const raucSupportConnectedPlugAppArmor = `
# Description: allows access to the RAUC update service on the system bus.
# This grants the ability to install bundles, mark slots good/bad, and inspect
# slot/artifact status via the de.pengutronix.rauc.Installer interface.

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/
    interface=de.pengutronix.rauc.Installer
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),

# receive property changes and the Completed signal from rauc
dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/
    interface=de.pengutronix.rauc.Installer
    peer=(label=###SLOT_SECURITY_TAGS###),

# allow the well-known name to be resolved on the bus
dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{GetNameOwner,NameHasOwner}"
    peer=(name=org.freedesktop.DBus, label=unconfined),
`

const raucSupportConnectedSlotAppArmor = `
dbus (receive)
    bus=system
    path=/
    interface=de.pengutronix.rauc.Installer
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(name=de.pengutronix.rauc, label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/
    interface=de.pengutronix.rauc.Installer
    peer=(name=de.pengutronix.rauc, label=###PLUG_SECURITY_TAGS###),
`

type raucSupportInterface struct{}

func (iface *raucSupportInterface) Name() string {
	return "rauc-support"
}

func (iface *raucSupportInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              raucSupportSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: raucSupportBaseDeclarationSlots,
	}
}

func (iface *raucSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if implicitSystemConnectedSlot(slot) {
		// rauc runs on the host as an unconfined system service
		new = "unconfined"
	} else {
		new = slot.LabelExpression()
	}
	snippet := strings.Replace(raucSupportConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *raucSupportInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if implicitSystemConnectedSlot(slot) {
		return nil
	}
	old := "###PLUG_SECURITY_TAGS###"
	new := plug.LabelExpression()
	snippet := strings.Replace(raucSupportConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *raucSupportInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&raucSupportInterface{})
}
