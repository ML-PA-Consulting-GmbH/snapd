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

	EncodedRepairRootAccountKeyPublicKeySha3           string
	EncodedStagingRepairRootAccountKeyPublicKeySha3    string
	EncodedCanonicalAccountPublicKeySha3               string
	EncodedCanonicalRootAccountKeyPublicKeySha3        string
	EncodedGenericAccountPublicKeySha3                 string
	EncodedGenericModelsAccountKeyPublicKeySha3        string
	EncodedGenericClassicModelPublicKeySha3            string
	EncodedStagingTrustedAccountPublicKeySha3          string
	EncodedStagingRootAccountKeyPublicKeySha3          string
	EncodedStagingGenericAccountPublicKeySha3          string
	EncodedStagingGenericModelsAccountKeyPublicKeySha3 string
	EncodedStagingGenericClassicModelPublicKeySha3     string
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

func GetProdId(name string) string {
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

func GetStagingId(name string) string {
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

func GetEncoded(name string) string {
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

	case "RepairRootAccountKeyPublicKeySha3":
		return values.EncodedRepairRootAccountKeyPublicKeySha3
	case "StagingRepairRootAccountKeyPublicKeySha3":
		return values.EncodedStagingRepairRootAccountKeyPublicKeySha3
	case "CanonicalAccountPublicKeySha3":
		return values.EncodedCanonicalAccountPublicKeySha3
	case "CanonicalRootAccountKeyPublicKeySha3":
		return values.EncodedCanonicalRootAccountKeyPublicKeySha3
	case "GenericAccountPublicKeySha3":
		return values.EncodedGenericAccountPublicKeySha3
	case "GenericModelsAccountKeyPublicKeySha3":
		return values.EncodedGenericModelsAccountKeyPublicKeySha3
	case "GenericClassicModelPublicKeySha3":
		return values.EncodedGenericClassicModelPublicKeySha3
	case "StagingTrustedAccountPublicKeySha3":
		return values.EncodedStagingTrustedAccountPublicKeySha3
	case "StagingRootAccountKeyPublicKeySha3":
		return values.EncodedStagingRootAccountKeyPublicKeySha3
	case "StagingGenericAccountPublicKeySha3":
		return values.EncodedStagingGenericAccountPublicKeySha3
	case "StagingGenericModelsAccountKeyPublicKeySha3":
		return values.EncodedStagingGenericModelsAccountKeyPublicKeySha3
	case "StagingGenericClassicModelPublicKeySha3":
		return values.EncodedStagingGenericClassicModelPublicKeySha3

	default:
		panic("Unknown encoded key: " + name)
	}
}
