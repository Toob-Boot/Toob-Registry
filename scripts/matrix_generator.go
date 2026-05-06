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

type HistoryEntry struct {
	CliVersion   string `json:"cli_version"`
	HardwareHash string `json:"hardware_hash"`
	Status       string `json:"status"`
	Timestamp    string `json:"timestamp"`
}

type MatrixChip struct {
	CurrentHardwareHash string            `json:"current_hardware_hash"`
	VerifiedCliVersions map[string]string `json:"verified_cli_versions"`
	History             []HistoryEntry    `json:"history"`
}

type Matrix map[string]MatrixChip

type Target struct {
	Chip string `json:"chip"`
	Cli  string `json:"cli"`
}

// getActiveCliVersions fetches tags from GitHub. Fallbacks to main.
func getActiveCliVersions() []string {
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
	for _, t := range tags {
		versions = append(versions, t.Name)
	}
	// Always append main as the bleeding edge test
	versions = append(versions, "main")

	if len(versions) == 0 {
		return []string{"main"}
	}
	return versions
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
		matrixEntry, exists := matrix[chipKey]
		var verifiedMap map[string]string
		
		if exists && matrixEntry.CurrentHardwareHash == hardwareHash {
			verifiedMap = matrixEntry.VerifiedCliVersions
		} else {
			// Hash changed (or new chip), reset verified map
			verifiedMap = make(map[string]string)
		}

		// Compare Cartesian Product
		for _, cli := range cliVersions {
			if verifiedMap[cli] != "VERIFIED" {
				testQueue = append(testQueue, Target{
					Chip: chipKey,
					Cli:  cli,
				})
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
