// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build testing

package branding

import (
	"os"
	"path/filepath"
)

// Test init: automatically load branding.yaml.example for tests.
// This file is compiled only when the "testing" build tag is set.
func init() {
	if BrandConfig != nil || configLoaded {
		return
	}
	if path, ok := findBrandingExample(); ok {
		_ = LoadConfigFromPath(path)
	}
}

func findBrandingExample() (string, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for range 25 {
		candidate := filepath.Join(wd, "branding", "branding.yaml.example")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", false
}
