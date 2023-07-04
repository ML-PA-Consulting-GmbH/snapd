package constants

import (
	"fmt"
	yaml "gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path"
	"sync"
)

type constants struct {
	BaseUrlSnapcraftDashboard        string
	BaseUrlSnapcraftDashboardStaging string
	BaseUrlSnapcraftApi              string
	BaseUrlSnapcraftStagingApi       string
	BaseUrlSnapcraftApiV2            string
	BaseUrlSnapcraftStagingApiV2     string

	AuthLocation        string
	AuthLocationStaging string

	AccountId         string
	HasGenericAccount bool

	ProdIdSnapd  string
	ProdIdCore   string
	ProdIdCore18 string
	ProdIdCore20 string
	ProdIdCore22 string

	StagingIdSnapd  string
	StagingIdCore   string
	StagingIdCore18 string
	StagingIdCore20 string
	StagingIdCore22 string

	EncodedRepairRootAccountKey           string
	EncodedStagingRepairRootAccountKey    string
	EncodedCanonicalAccount               string
	EncodedCanonicalRootAccountKey        string
	EncodedGenericAccount                 string
	EncodedGenericModelsAccountKey        string
	EncodedGenericClassicModel            string
	EncodedStagingTrustedAccount          string
	EncodedStagingRootAccountKey          string
	EncodedStagingGenericAccount          string
	EncodedStagingGenericModelsAccountKey string
	EncodedStagingGenericClassicModel     string
}

var initOnce sync.Once
var values constants

func doInit() {
	fmt.Println("Loading constants...")
	snapDir, exists := os.LookupEnv("SNAP")

	if !exists || snapDir == "" {
		panic("$SNAP is empty, cannot initialize values.")
	}

	data, err := ioutil.ReadFile(path.Join(snapDir, "constants.yaml"))
	if err != nil {
		panic(fmt.Sprintf("Failed to read constants.yaml: %v", err.Error()))
	}

	err = yaml.Unmarshal(data, &values)
	if err != nil {
		panic(fmt.Sprintf("Failed to unmarshal constants.yaml: %v", err.Error()))
	}

}

func GetBaseUrl(name string) string {
	initOnce.Do(doInit)
	switch name {
	case "SnapcraftDashboard":
		return values.BaseUrlSnapcraftDashboard
	case "SnapcraftDashboardStaging":
		return values.BaseUrlSnapcraftDashboardStaging
	case "SnapcraftApi":
		return values.BaseUrlSnapcraftApi
	case "SnapcraftStagingApi":
		return values.BaseUrlSnapcraftStagingApi
	case "SnapcraftApiV2":
		return values.BaseUrlSnapcraftApiV2
	case "SnapcraftStagingApiV2":
		return values.BaseUrlSnapcraftStagingApiV2
	case "AuthLocation":
		return values.AuthLocation
	case "AuthLocationStaging":
		return values.AuthLocationStaging
	default:
		panic("Unknown base url: " + name)
	}
}

func GetAuthLocation() string {
	initOnce.Do(doInit)
	return values.AuthLocation
}

func GetAuthLocationStaging() string {
	initOnce.Do(doInit)
	return values.AuthLocationStaging
}

func GetAccountId() string {
	initOnce.Do(doInit)
	return values.AccountId
}

func GetHasGenericAccount() bool {
	initOnce.Do(doInit)
	return values.HasGenericAccount
}

func GetProdSnapId(name string) string {
	initOnce.Do(doInit)
	switch name {
	case "snapd":
		return values.ProdIdSnapd
	case "core":
		return values.ProdIdCore
	case "core18":
		return values.ProdIdCore18
	case "core20":
		return values.ProdIdCore20
	case "core22":
		return values.ProdIdCore22
	default:
		panic("Unknown snap id: " + name)
	}
}

func GetStagingSnapId(name string) string {
	initOnce.Do(doInit)
	switch name {
	case "snapd":
		return values.StagingIdSnapd
	case "core":
		return values.StagingIdCore
	case "core18":
		return values.StagingIdCore18
	case "core20":
		return values.StagingIdCore20
	case "core22":
		return values.StagingIdCore22
	default:
		panic("Unknown snap id: " + name)
	}
}

func GetEncodedKey(name string) string {
	initOnce.Do(doInit)
	switch name {
	case "RepairRootAccountKey":
		return values.EncodedRepairRootAccountKey
	case "StagingRepairRootAccountKey":
		return values.EncodedStagingRepairRootAccountKey
	case "CanonicalAccount":
		return values.EncodedCanonicalAccount
	case "CanonicalRootAccountKey":
		return values.EncodedCanonicalRootAccountKey
	case "GenericAccount":
		return values.EncodedGenericAccount
	case "GenericModelsAccountKey":
		return values.EncodedGenericModelsAccountKey
	case "GenericClassicModel":
		return values.EncodedGenericClassicModel
	case "StagingTrustedAccount":
		return values.EncodedStagingTrustedAccount
	case "StagingRootAccountKey":
		return values.EncodedStagingRootAccountKey
	case "StagingGenericAccount":
		return values.EncodedStagingGenericAccount
	case "StagingGenericModelsAccountKey":
		return values.EncodedStagingGenericModelsAccountKey
	case "StagingGenericClassicModel":
		return values.EncodedStagingGenericClassicModel
	default:
		panic("Unknown encoded key: " + name)
	}
}
