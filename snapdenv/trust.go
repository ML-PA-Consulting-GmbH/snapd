// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 ML!PA Consulting GmbH
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 */

package snapdenv

// TrustLevel controls how strictly assertion validation and model
// grade restrictions are enforced. In a closed-ecosystem deployment
// (private snap store, pre-signed snaps) the image builder can relax
// these checks because all content is already trusted.
type TrustLevel int

const (
	// TrustStrict is the default: all assertions are verified,
	// model grade restrictions are enforced, and extra assertion
	// types are limited.
	TrustStrict TrustLevel = iota

	// TrustInsecure disables assertion signature checks, model
	// grade restrictions, and assertion type filtering. Use only
	// in closed ecosystems where all inputs are pre-validated.
	TrustInsecure
)

var currentTrustLevel TrustLevel = TrustStrict

// SetTrustLevel sets the global trust level. Should be called once,
// early in the process lifetime (e.g. from the image builder).
func SetTrustLevel(level TrustLevel) {
	currentTrustLevel = level
}

// GetTrustLevel returns the current global trust level.
func GetTrustLevel() TrustLevel {
	return currentTrustLevel
}

// Insecure returns true when assertion validation should be skipped.
func Insecure() bool {
	return currentTrustLevel >= TrustInsecure
}
