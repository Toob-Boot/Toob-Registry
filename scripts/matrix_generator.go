package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

type ChipManifest struct {
	Name           string `json:"name"`
	Vendor         string `json:"vendor"`
	Arch           string `json:"arch"`
	CompilerPrefix string `json:"compiler_prefix"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	Path           string `json:"path,omitempty"`
	MinCoreSDK     string `json:"min_core_sdk,omitempty"`
	MinCompiler    string `json:"min_compiler,omitempty"`
}

type ToolchainEntry struct {
	Version string            `json:"version"`
	Urls    map[string]string `json:"urls"`
	Sha256  map[string]string `json:"sha256"`
}

type VendorEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type ArchEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Registry struct {
	Chips      map[string]ChipManifest   `json:"chips"`
	Toolchains map[string]ToolchainEntry `json:"toolchains"`
	Vendors    map[string]VendorEntry    `json:"vendors"`
	Archs      map[string]ArchEntry      `json:"archs"`
}

type Dependencies struct {
	Toolchain string `json:"toolchain"`
	Vendor    string `json:"vendor"`
	Arch      string `json:"arch"`
}

type VerifiedCombination struct {
	Status     string `json:"status"`
	LastTested string `json:"last_tested"`
}

type MatrixVersion struct {
	EnvironmentHash     string                           `json:"environment_hash"`
	Dependencies        Dependencies                     `json:"dependencies"`
	VerifiedCombinations map[string]VerifiedCombination `json:"verified_combinations"`
}

type MatrixChip struct {
	Versions map[string]MatrixVersion `json:"versions"`
}

type Matrix map[string]MatrixChip

type Target struct {
	Chip     string `json:"chip"`
	Cli      string `json:"cli"`
	Core     string `json:"core"`
	Compiler string `json:"compiler"`
}

// getActiveCliVersions fetches releases from GitHub. Fallbacks to main.
func getActiveCliVersions() []string {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Toob-Boot/Toob-CLI-Release/releases", nil)
	if err != nil {
		return []string{"main"}
	}
	req.Header.Set("User-Agent", "Toob-Registry-Matrix-Generator")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return []string{"main"}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return []string{"main"}
	}

	var versions []string
	for _, rel := range releases {
		versions = append(versions, rel.TagName)
	}
	// Always append main as the bleeding edge test
	versions = append(versions, "main")

	if len(versions) == 0 {
		return []string{"main"}
	}
	return versions
}

// getActiveCoreVersions fetches tags from Toob-Loader repo. Fallbacks to main.
func getActiveCoreVersions() []string {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Toob-Boot/Toob-Loader/tags", nil)
	if err != nil {
		return []string{"main"}
	}
	req.Header.Set("User-Agent", "Toob-Registry-Matrix-Generator")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return []string{"main"}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return []string{"main"}
	}

	var versions []string
	for _, tag := range tags {
		if len(tag.Name) > 5 && tag.Name[:5] == "core/" {
			versions = append(versions, tag.Name)
		}
	}
	// Always append main
	versions = append(versions, "main")
	return versions
}

// getActiveCompilerVersions returns the current compiler tags to test.
func getActiveCompilerVersions() []string {
	return []string{"latest"}
}

func main() {
	regData, err := os.ReadFile("registry.json")
	if err != nil {
		log.Fatalf("FATAL: Error reading registry.json: %v", err)
	}

	var registry Registry
	if err := json.Unmarshal(regData, &registry); err != nil {
		log.Fatalf("FATAL: Error parsing registry.json: %v", err)
	}

	matrixData, err := os.ReadFile("compatibility_matrix.json")
	if err != nil && !os.IsNotExist(err) {
		log.Fatalf("FATAL: Error reading compatibility_matrix.json: %v", err)
	}

	var matrix Matrix
	if len(matrixData) > 0 {
		if err := json.Unmarshal(matrixData, &matrix); err != nil {
			log.Fatalf("FATAL: Error parsing compatibility_matrix.json: %v", err)
		}
	} else {
		matrix = make(Matrix)
	}

	targetChip := os.Getenv("CHIP")
	cliVersions := getActiveCliVersions()
	coreVersions := getActiveCoreVersions()
	compilerVersions := getActiveCompilerVersions()
	testQueue := []Target{}

	for chipKey, chip := range registry.Chips {
		tcName := chip.CompilerPrefix
		if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
			tcName = tcName[:len(tcName)-1]
		}
		
		toolchain, exists := registry.Toolchains[tcName]
		if !exists {
			continue
		}

		// Calculate pristine Hardware Hash (WITHOUT CLI version)
		vManifest := registry.Vendors[chip.Vendor]
		aManifest := registry.Archs[chip.Arch]
		
		chipBytes, _ := json.Marshal(chip)
		tcBytes, _ := json.Marshal(toolchain)
		vBytes, _ := json.Marshal(vManifest)
		aBytes, _ := json.Marshal(aManifest)
		
		h := sha256.New()
		h.Write(chipBytes)
		h.Write(tcBytes)
		h.Write(vBytes)
		h.Write(aBytes)
		hardwareHash := hex.EncodeToString(h.Sum(nil))

		// If called with specific CHIP, just output its hash and exit
		if targetChip == chipKey {
			fmt.Print(hardwareHash)
			os.Exit(0)
		}

		// Calculate T - A (Target minus Actual)
		matrixEntry, chipExists := matrix[chipKey]
		var verifiedMap map[string]VerifiedCombination
		
		if chipExists {
			if versionEntry, versionExists := matrixEntry.Versions[chip.Version]; versionExists && versionEntry.EnvironmentHash == hardwareHash {
				if versionEntry.VerifiedCombinations != nil {
					verifiedMap = versionEntry.VerifiedCombinations
				} else {
					verifiedMap = make(map[string]VerifiedCombination)
				}
			} else {
				verifiedMap = make(map[string]VerifiedCombination)
			}
		} else {
			verifiedMap = make(map[string]VerifiedCombination)
		}

		// Compare Cartesian Product
		for _, cli := range cliVersions {
			for _, core := range coreVersions {
				for _, compiler := range compilerVersions {
					// Build tuple key
					tupleKey := fmt.Sprintf("cli=%s::core=%s::compiler=%s", cli, core, compiler)
					
					// Simple filter: if core != main and min_core_sdk is "main", skip older versions for now
					// (A real semver check could be implemented here later)
					if chip.MinCoreSDK == "main" && core != "main" {
						continue
					}

					if verifiedMap[tupleKey].Status != "VERIFIED" {
						testQueue = append(testQueue, Target{
							Chip:     chipKey,
							Cli:      cli,
							Core:     core,
							Compiler: compiler,
						})
					}
				}
			}
		}
	}

	if targetChip != "" {
		fmt.Print("dummyhash")
		os.Exit(1)
	}

	out, _ := json.Marshal(testQueue)
	fmt.Println(string(out))
}
