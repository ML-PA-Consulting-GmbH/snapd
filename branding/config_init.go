// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !testing

package branding

// Production init: config is loaded explicitly from main() via LoadConfig().
// This file is compiled when the "testing" build tag is NOT set.
func init() {
	// No-op in production. LoadConfig() is called explicitly from main().
}
