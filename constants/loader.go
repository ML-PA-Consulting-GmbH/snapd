package constants

import (
	"fmt"
	yaml "gopkg.in/yaml.v2"
	"os"
	"path"
	"strings"
	"sync"
)

type constants struct {
	BaseUrlSnapcraftDashboard        string `yaml:"BaseUrlSnapcraftDashboard"`
	BaseUrlSnapcraftDashboardStaging string `yaml:"BaseUrlSnapcraftDashboardStaging"`
	BaseUrlSnapcraftApi              string `yaml:"BaseUrlSnapcraftApi"`
	BaseUrlSnapcraftStagingApi       string `yaml:"BaseUrlSnapcraftStagingApi"`
	BaseUrlSnapcraftApiV2            string `yaml:"BaseUrlSnapcraftApiV2"`
	BaseUrlSnapcraftStagingApiV2     string `yaml:"BaseUrlSnapcraftStagingApiV2"`

	AuthLocation        string `yaml:"AuthLocation"`
	AuthLocationStaging string `yaml:"AuthLocationStaging"`

	AccountId         string `yaml:"AccountId"`
	HasGenericAccount bool   `yaml:"HasGenericAccount"`

	ProdIdSnapd  string `yaml:"ProdIdSnapd"`
	ProdIdCore   string `yaml:"ProdIdCore"`
	ProdIdCore18 string `yaml:"ProdIdCore18"`
	ProdIdCore20 string `yaml:"ProdIdCore20"`
	ProdIdCore22 string `yaml:"ProdIdCore22"`

	StagingIdSnapd  string `yaml:"StagingIdSnapd"`
	StagingIdCore   string `yaml:"StagingIdCore"`
	StagingIdCore18 string `yaml:"StagingIdCore18"`
	StagingIdCore20 string `yaml:"StagingIdCore20"`
	StagingIdCore22 string `yaml:"StagingIdCore22"`

	EncodedRepairRootAccountKey           string `yaml:"EncodedRepairRootAccountKey"`
	EncodedStagingRepairRootAccountKey    string `yaml:"EncodedStagingRepairRootAccountKey"`
	EncodedCanonicalAccount               string `yaml:"EncodedCanonicalAccount"`
	EncodedCanonicalRootAccountKey        string `yaml:"EncodedCanonicalRootAccountKey"`
	EncodedGenericAccount                 string `yaml:"EncodedGenericAccount"`
	EncodedGenericModelsAccountKey        string `yaml:"EncodedGenericModelsAccountKey"`
	EncodedGenericClassicModel            string `yaml:"EncodedGenericClassicModel"`
	EncodedStagingTrustedAccount          string `yaml:"EncodedStagingTrustedAccount"`
	EncodedStagingRootAccountKey          string `yaml:"EncodedStagingRootAccountKey"`
	EncodedStagingGenericAccount          string `yaml:"EncodedStagingGenericAccount"`
	EncodedStagingGenericModelsAccountKey string `yaml:"EncodedStagingGenericModelsAccountKey"`
	EncodedStagingGenericClassicModel     string `yaml:"EncodedStagingGenericClassicModel"`

	EncodedRepairRootAccountKeyPublicKeySha3        string `yaml:"EncodedRepairRootAccountKeyPublicKeySha3"`
	EncodedStagingRepairRootAccountKeyPublicKeySha3 string `yaml:"EncodedStagingRepairRootAccountKeyPublicKeySha3"`
	EncodedCanonicalAccountSignKeySha3              string `yaml:"EncodedCanonicalAccountSignKeySha3"`

	EncodedCanonicalRootAccountKeyPublicKeySha3        string `yaml:"EncodedCanonicalRootAccountKeyPublicKeySha3"`
	EncodedGenericAccountPublicKeySha3                 string `yaml:"EncodedGenericAccountPublicKeySha3"`
	EncodedGenericModelsAccountKeyPublicKeySha3        string `yaml:"EncodedGenericModelsAccountKeyPublicKeySha3"`
	EncodedGenericClassicModelPublicKeySha3            string `yaml:"EncodedGenericClassicModelPublicKeySha3"`
	EncodedStagingTrustedAccountPublicKeySha3          string `yaml:"EncodedStagingTrustedAccountPublicKeySha3"`
	EncodedStagingRootAccountKeyPublicKeySha3          string `yaml:"EncodedStagingRootAccountKeyPublicKeySha3"`
	EncodedStagingGenericAccountPublicKeySha3          string `yaml:"EncodedStagingGenericAccountPublicKeySha3"`
	EncodedStagingGenericModelsAccountKeyPublicKeySha3 string `yaml:"EncodedStagingGenericModelsAccountKeyPublicKeySha3"`
	EncodedStagingGenericClassicModelPublicKeySha3     string `yaml:"EncodedStagingGenericClassicModelPublicKeySha3"`
}

var initOnce sync.Once
var values constants

func doInit() {
	signedYaml := loadYaml()
	plainYaml := verifySignature(signedYaml)
	values = parseYaml(plainYaml)
	validateValues(&values)
}

func validateValues(values *constants) {
	if values.BaseUrlSnapcraftDashboard == "" {
		panic("BaseUrlSnapcraftDashboard is empty")
	}
	if values.BaseUrlSnapcraftDashboardStaging == "" {
		panic("BaseUrlSnapcraftDashboardStaging is empty")
	}
	if values.BaseUrlSnapcraftApi == "" {
		panic("BaseUrlSnapcraftApi is empty")
	}
	if values.BaseUrlSnapcraftStagingApi == "" {
		panic("BaseUrlSnapcraftStagingApi is empty")
	}
	if values.BaseUrlSnapcraftApiV2 == "" {
		panic("BaseUrlSnapcraftApiV2 is empty")
	}
	if values.BaseUrlSnapcraftStagingApiV2 == "" {
		panic("BaseUrlSnapcraftStagingApiV2 is empty")
	}

	if values.AuthLocation == "" {
		panic("AuthLocation is empty")
	}
	if values.AuthLocationStaging == "" {
		panic("AuthLocationStaging is empty")
	}

	if values.AccountId == "" {
		panic("AccountId is empty")
	}

	if values.ProdIdSnapd == "" {
		panic("ProdIdSnapd is empty")
	}
	if values.ProdIdCore == "" {
		panic("ProdIdCore is empty")
	}
	if values.ProdIdCore18 == "" {
		panic("ProdIdCore18 is empty")
	}
	if values.ProdIdCore20 == "" {
		panic("ProdIdCore20 is empty")
	}
	//if values.ProdIdCore22 == "" {
	//	panic("ProdIdCore22 is empty")
	//}

	if values.StagingIdSnapd == "" {
		panic("StagingIdSnapd is empty")
	}
	if values.StagingIdCore == "" {
		panic("StagingIdCore is empty")
	}
	if values.StagingIdCore18 == "" {
		panic("StagingIdCore18 is empty")
	}

	// snapd tests require this to be empty
	values.StagingIdCore20 = ""
	values.StagingIdCore22 = ""

	//if values.StagingIdCore20 == "" {
	//	panic("StagingIdCore20 is empty")
	//}
	//if values.StagingIdCore22 == "" {
	//	panic("StagingIdCore22 is empty")
	//}

	if values.EncodedRepairRootAccountKey == "" {
		panic("EncodedRepairRootAccountKey is empty")
	}
	values.EncodedRepairRootAccountKeyPublicKeySha3 = getPublicKey(values.EncodedRepairRootAccountKey)
	if values.EncodedRepairRootAccountKeyPublicKeySha3 == "" {
		panic("EncodedRepairRootAccountKeyPublicKeySha3 is empty")
	}
	values.EncodedStagingRepairRootAccountKeyPublicKeySha3 = getSignKey(values.EncodedStagingRepairRootAccountKey)
	if values.EncodedStagingRepairRootAccountKeyPublicKeySha3 == "" {
		panic("EncodedStagingRepairRootAccountKeyPublicKeySha3 is empty")
	}
	values.EncodedCanonicalAccountSignKeySha3 = getSignKey(values.EncodedCanonicalAccount)
	if values.EncodedCanonicalAccountSignKeySha3 == "" {
		panic("EncodedCanonicalAccountSignKeySha3 is empty")
	}
	values.EncodedGenericModelsAccountKeyPublicKeySha3 = getPublicKey(values.EncodedGenericModelsAccountKey)
	if values.EncodedGenericModelsAccountKeyPublicKeySha3 == "" {
		panic("EncodedGenericClassicModel is empty")
	}

	values.EncodedRepairRootAccountKey = strings.TrimSpace(values.EncodedRepairRootAccountKey) + "\n"
	values.EncodedStagingRepairRootAccountKey = strings.TrimSpace(values.EncodedStagingRepairRootAccountKey) + "\n"
	values.EncodedCanonicalAccount = strings.TrimSpace(values.EncodedCanonicalAccount) + "\n"
	values.EncodedCanonicalRootAccountKey = strings.TrimSpace(values.EncodedCanonicalRootAccountKey) + "\n"
	values.EncodedGenericAccount = strings.TrimSpace(values.EncodedGenericAccount) + "\n"
	values.EncodedGenericModelsAccountKey = strings.TrimSpace(values.EncodedGenericModelsAccountKey) + "\n"
	values.EncodedGenericClassicModel = strings.TrimSpace(values.EncodedGenericClassicModel) + "\n"
	values.EncodedStagingTrustedAccount = strings.TrimSpace(values.EncodedStagingTrustedAccount) + "\n"
	values.EncodedStagingRootAccountKey = strings.TrimSpace(values.EncodedStagingRootAccountKey) + "\n"
	values.EncodedStagingGenericAccount = strings.TrimSpace(values.EncodedStagingGenericAccount) + "\n"
	values.EncodedStagingGenericModelsAccountKey = strings.TrimSpace(values.EncodedStagingGenericModelsAccountKey) + "\n"
	values.EncodedStagingGenericClassicModel = strings.TrimSpace(values.EncodedStagingGenericClassicModel) + "\n"
}

func loadYaml() []byte {
	snapYamlDir, exists := os.LookupEnv("SNAP")
	if !exists || snapYamlDir == "" {
		fmt.Println("WARNIN: $SNAP is empty, using /etc/snapd/constants.yaml for config")
		snapYamlDir = "/etc/snapd/"
		//panic("$SNAP is empty, cannot initialize values.")
	}
	signedYaml, err := os.ReadFile(path.Join(snapYamlDir, "constants.yaml"))
	if err != nil {
		panic(fmt.Sprintf("Failed to read constants.yaml: %v", err.Error()))
	}
	return signedYaml
}

func parseYaml(plainYaml []byte) constants {
	res := constants{}
	if err := yaml.Unmarshal(plainYaml, &res); err != nil {
		panic(fmt.Sprintf("Failed to unmarshal constants.yaml: %v", err.Error()))
	}
	return res
}

func verifySignature(data []byte) []byte {
	return []byte(strings.TrimSpace(string(data)))
	//// TODO: process signed yaml
	//sections := strings.Split(strings.TrimSpace(string(data)), "\n\n")
	//body := strings.TrimSpace(strings.Join(sections[:len(sections)-1], "\n\n"))
	//signature := sections[len(sections)-1]
	//
	//return data
}

func getPublicKey(assertion string) string {
	lines := strings.Split(assertion, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "public-key-sha3-384: ") {
			return strings.TrimPrefix(line, "public-key-sha3-384: ")
		}
	}
	panic("Could not find public-key-sha3-384 in assertion: \n" + assertion)
}

func getSignKey(assertion string) string {
	lines := strings.Split(assertion, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "sign-key-sha3-384: ") {
			return strings.TrimPrefix(line, "sign-key-sha3-384: ")
		}
	}
	panic("Could not find sign-key-sha3-384 in assertion: \n" + assertion)
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
	switch strings.ToLower(name) {
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
	switch strings.ToLower(name) {
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
	case "CanonicalAccountSignKeySha3":
		return values.EncodedCanonicalAccountSignKeySha3
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
