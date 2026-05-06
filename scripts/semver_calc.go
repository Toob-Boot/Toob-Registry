package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// Simplified manifest structures
type ChipManifest struct {
	Name           string `json:"name"`
	Vendor         string `json:"vendor"`
	Arch           string `json:"arch"`
	CompilerPrefix string `json:"compiler_prefix"`
	Description    string `json:"description"`
	Version        string `json:"version"`
}

type DependencyManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type MatrixVersion struct {
	HardwareHash string `json:"hardware_hash"`
}

type MatrixChip struct {
	Versions map[string]MatrixVersion `json:"versions"`
}

type Matrix map[string]MatrixChip

func parseSemVer(v string) (major, minor, patch int) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return
}

func calculateHardwareHash(chip ChipManifest, tc, v, a interface{}) string {
	chipBytes, _ := json.Marshal(chip)
	tcBytes, _ := json.Marshal(tc)
	vBytes, _ := json.Marshal(v)
	aBytes, _ := json.Marshal(a)

	h := sha256.New()
	h.Write(chipBytes)
	h.Write(tcBytes)
	h.Write(vBytes)
	h.Write(aBytes)
	return hex.EncodeToString(h.Sum(nil))
}

func main() {
	fmt.Println("Running Toob SemVer & Dependency Resolver...")

	// 1. Read the compatibility matrix to get previous hardware hashes
	matrixData, err := os.ReadFile("compatibility_matrix.json")
	var matrix Matrix
	if err == nil && len(matrixData) > 0 {
		json.Unmarshal(matrixData, &matrix)
	}

	// 2. Read Registry build state
	regData, err := os.ReadFile("registry.json")
	if err != nil {
		log.Fatalf("Cannot run semver resolver without built registry.json")
	}

	var reg struct {
		Chips      map[string]ChipManifest       `json:"chips"`
		Toolchains map[string]DependencyManifest `json:"toolchains"`
		Vendors    map[string]DependencyManifest `json:"vendors"`
		Archs      map[string]DependencyManifest `json:"archs"`
	}
	json.Unmarshal(regData, &reg)

	changesMade := false

	// 3. For each chip, calculate current hardware hash
	for chipKey, chip := range reg.Chips {
		tcName := chip.CompilerPrefix
		if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
			tcName = tcName[:len(tcName)-1]
		}
		
		tc := reg.Toolchains[tcName]
		vendor := reg.Vendors[chip.Vendor]
		arch := reg.Archs[chip.Arch]

		currentHash := calculateHardwareHash(chip, tc, vendor, arch)

		// Check if this version exists in matrix and the hash has changed
		matrixChip, chipHasHistory := matrix[chipKey]
		if !chipHasHistory {
			continue // New chip, nothing to bump
		}

		versionEntry, versionExists := matrixChip.Versions[chip.Version]
		
		// If the version exists in the matrix, but the hardware hash has CHANGED, 
		// it means a dependency updated since the last matrix run. We must bump the chip!
		if versionExists && versionEntry.HardwareHash != currentHash {
			fmt.Printf("[Bump Required] Chip '%s' (v%s) dependencies changed!\n", chipKey, chip.Version)
			
			// For now, if a dependency changes, we enforce a MINOR bump on the chip.
			// (In a full git-hook, we'd compare old tc.Version vs new tc.Version)
			major, minor, patch := parseSemVer(chip.Version)
			minor++
			patch = 0
			newVersion := fmt.Sprintf("%d.%d.%d", major, minor, patch)
			
			fmt.Printf(" -> Bumping '%s' to v%s\n", chipKey, newVersion)
			
			// Open the chip manifest and rewrite it
			manifestPath := fmt.Sprintf("chips/%s/chip_manifest.json", chipKey)
			mBytes, err := os.ReadFile(manifestPath)
			if err == nil {
				var raw map[string]interface{}
				json.Unmarshal(mBytes, &raw)
				raw["version"] = newVersion
				
				out, _ := json.MarshalIndent(raw, "", "    ")
				os.WriteFile(manifestPath, out, 0644)
				changesMade = true
			}
		}
	}

	if changesMade {
		fmt.Println("Successfully resolved and bumped dependent SemVer manifests.")
		// We would ideally rebuild the registry.json here so the next steps pick up the new chip version.
	} else {
		fmt.Println("All dependent SemVer manifests are up to date.")
	}
}
