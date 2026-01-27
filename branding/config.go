// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014,2015,2017 Canonical Ltd
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

package branding

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BrandingConfig represents the snapd brand configuration loaded from a YAML file.
// This configuration is typically embedded in the snapd snap or kernel snap
// and defines store endpoints, account assertions, and snap IDs.
type BrandingConfig struct {
	Store      StoreConfig      `yaml:"store"`
	SnapIDs    SnapIDsConfig    `yaml:"snap_ids"`
	Assertions AssertionsConfig `yaml:"assertions"`
	Models     ModelsConfig     `yaml:"models"`
	KeySHA3    KeySHA3Config    `yaml:"key_sha3"`
}

// StoreConfig contains store endpoint URLs and account information.
type StoreConfig struct {
	DashboardURL        string `yaml:"dashboard_url"`
	APIURL              string `yaml:"api_url"`
	APIV2URL            string `yaml:"api_v2_url"`
	AuthLocation        string `yaml:"auth_location"`
	StoreOwnerAccountID string `yaml:"store_owner_account_id"`
}

// SnapIDsConfig contains snap store IDs for core snaps.
type SnapIDsConfig struct {
	Snapd  string `yaml:"snapd"`
	Core   string `yaml:"core"`
	Core18 string `yaml:"core18"`
	Core20 string `yaml:"core20"`
	Core22 string `yaml:"core22"`
	Core24 string `yaml:"core24"`
	Core26 string `yaml:"core26"`
}

// AssertionsConfig contains encoded assertion strings.
type AssertionsConfig struct {
	StoreOwnerAccount string `yaml:"store_owner_account"`
	RootAccountKey    string `yaml:"root_account_key"`
	RepairAccountKey  string `yaml:"repair_account_key"`
	ModelsAccountKey  string `yaml:"models_account_key"`
}

// ModelsConfig contains optional model assertions.
type ModelsConfig struct {
	GenericClassicModel string `yaml:"generic_classic_model"`
}

// KeySHA3Config contains SHA3-384 hashes of public keys for quick lookups.
type KeySHA3Config struct {
	StoreOwnerSignKey string `yaml:"store_owner_sign_key"`
	RepairPublicKey   string `yaml:"repair_public_key"`
	ModelsPublicKey   string `yaml:"models_public_key"`
}

// configPaths defines the search order for the branding configuration file.
// The first existing file is used.
var configPaths = []string{
	"/snap/snapd/current/branding.yaml", // mounted snapd snap
	"/var/lib/snapd/branding.yaml",      // initrd fallback
}

// BrandConfig is the loaded branding configuration. It is nil until LoadConfig is called.
var BrandConfig *BrandingConfig

// configLoaded tracks whether LoadConfig has been called successfully.
var configLoaded bool

// LoadConfig loads the brand configuration from the first available config file.
// It panics if no configuration file is found or if the file is invalid.
// This function should be called early in snapd's main() before any other
// package initialization that depends on these constants.
func LoadConfig() {
	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			if err := loadConfigFromFile(path); err != nil {
				panic(fmt.Sprintf("failed to load snapd config from %s: %v", path, err))
			}
			configLoaded = true
			return
		}
	}
	panic(fmt.Sprintf("no snapd configuration file found; searched: %v", configPaths))
}

// LoadConfigFromPath loads configuration from a specific path.
// This is useful for testing or when the config location is known.
func LoadConfigFromPath(path string) error {
	if err := loadConfigFromFile(path); err != nil {
		return err
	}
	configLoaded = true
	return nil
}

// loadConfigFromFile reads and parses the YAML config file, then populates
// the package-level variables.
func loadConfigFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read config file: %w", err)
	}

	var cfg BrandingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("cannot parse config file: %w", err)
	}

	BrandConfig = &cfg
	return nil
}

// IsConfigLoaded returns true if LoadConfig has been called successfully.
func IsConfigLoaded() bool {
	return configLoaded
}

// SetConfigPaths allows overriding the default config search paths.
// This is primarily useful for testing. Returns a restore function.
func SetConfigPaths(paths []string) (restore func()) {
	oldPaths := configPaths
	configPaths = paths
	return func() {
		configPaths = oldPaths
	}
}
