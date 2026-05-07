package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

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
	EnvironmentHash      string                         `json:"environment_hash"`
	Dependencies         Dependencies                   `json:"dependencies"`
	VerifiedCombinations map[string]VerifiedCombination `json:"verified_combinations"`
}

type MatrixChip struct {
	Versions map[string]MatrixVersion `json:"versions"`
}

type Matrix map[string]MatrixChip

type Result struct {
	Chip            string `json:"chip"`
	CliVersion      string `json:"cli_version"`
	CoreVersion     string `json:"core_version"`
	CompilerVersion string `json:"compiler_version"`
	Status          string `json:"status"`
	StateHash       string `json:"state_hash"`
}

type InternalStateEntry struct {
	Status     string `json:"status"`
	LastTested string `json:"last_tested"`
	RetryCount int    `json:"retry_count"`
}

type InternalState struct {
	Combinations map[string]InternalStateEntry `json:"combinations"`
}

func main() {
	matrixData, err := os.ReadFile("compatibility_matrix.json")
	if err != nil {
		log.Fatalf("FATAL: Error reading matrix: %v", err)
	}

	var matrix Matrix
	if len(matrixData) > 0 {
		if err := json.Unmarshal(matrixData, &matrix); err != nil {
			log.Fatalf("FATAL: Error parsing matrix: %v", err)
		}
	} else {
		matrix = make(Matrix)
	}

	// Read all result_*.json files
	matches, err := filepath.Glob("result_*.json")
	if err != nil {
		log.Fatalf("FATAL: Error globbing results: %v", err)
	}

	if len(matches) == 0 {
		fmt.Println("No results found to merge.")
		return
	}

	// Read internal_state.json
	stateData, _ := os.ReadFile("internal_state.json")
	var internalState InternalState
	if len(stateData) > 0 {
		json.Unmarshal(stateData, &internalState)
	}
	if internalState.Combinations == nil {
		internalState.Combinations = make(map[string]InternalStateEntry)
	}
	stateUpdated := false

	// Read registry.json once instead of inside the loop
	regData, err := os.ReadFile("registry.json")
	if err != nil {
		log.Fatalf("FATAL: Error reading registry: %v", err)
	}
	var reg struct {
		Chips map[string]struct {
			Version        string `json:"version"`
			Vendor         string `json:"vendor"`
			Arch           string `json:"arch"`
			CompilerPrefix string `json:"compiler_prefix"`
		} `json:"chips"`
		Toolchains map[string]struct {
			Version string `json:"version"`
		} `json:"toolchains"`
		Vendors map[string]struct {
			Version string `json:"version"`
		} `json:"vendors"`
		Archs map[string]struct {
			Version string `json:"version"`
		} `json:"archs"`
	}
	if err := json.Unmarshal(regData, &reg); err != nil {
		log.Fatalf("FATAL: Error parsing registry: %v", err)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	updated := false

	for _, file := range matches {
		data, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Warning: Failed to read %s: %v", file, err)
			continue
		}

		var res Result
		if err := json.Unmarshal(data, &res); err != nil {
			log.Printf("Warning: Failed to parse %s: %v", file, err)
			continue
		}

		if res.Chip == "" || res.StateHash == "" || res.CliVersion == "" {
			continue
		}

		if res.CoreVersion == "" {
			res.CoreVersion = "main"
		}
		if res.CompilerVersion == "" {
			res.CompilerVersion = "latest"
		}

		chipEntry, chipExists := matrix[res.Chip]
		if !chipExists {
			chipEntry = MatrixChip{
				Versions: make(map[string]MatrixVersion),
			}
			matrix[res.Chip] = chipEntry
		}

		chipManifestVer := "1.0.0"
		var chipMetaDeps Dependencies

		if chipMeta, ok := reg.Chips[res.Chip]; ok {
			chipManifestVer = chipMeta.Version

			// Resolve dependencies from registry
			tcName := chipMeta.CompilerPrefix
			if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
				tcName = tcName[:len(tcName)-1]
			}

			tcVer := "unknown"
			if tc, ok := reg.Toolchains[tcName]; ok {
				tcVer = tc.Version
			}

			vVer := "unknown"
			if v, ok := reg.Vendors[chipMeta.Vendor]; ok {
				vVer = v.Version
			}

			aVer := "unknown"
			if a, ok := reg.Archs[chipMeta.Arch]; ok {
				aVer = a.Version
			}

			chipMetaDeps = Dependencies{
				Toolchain: fmt.Sprintf("%s@%s", tcName, tcVer),
				Vendor:    fmt.Sprintf("%s@%s", chipMeta.Vendor, vVer),
				Arch:      fmt.Sprintf("%s@%s", chipMeta.Arch, aVer),
			}
		}

		versionEntry, versionExists := chipEntry.Versions[chipManifestVer]
		if !versionExists || versionEntry.EnvironmentHash != res.StateHash {
			versionEntry = MatrixVersion{
				EnvironmentHash:      res.StateHash,
				Dependencies:         chipMetaDeps,
				VerifiedCombinations: make(map[string]VerifiedCombination),
			}
		}

		if versionEntry.VerifiedCombinations == nil {
			versionEntry.VerifiedCombinations = make(map[string]VerifiedCombination)
		}

		tupleKey := fmt.Sprintf("chip=%s::cli=%s::core=%s::compiler=%s", res.Chip, res.CliVersion, res.CoreVersion, res.CompilerVersion)

		// Sweep and Prune: remove any existing non-VERIFIED statuses to reduce JSON bloat
		for key, comb := range versionEntry.VerifiedCombinations {
			if comb.Status != "VERIFIED" {
				delete(versionEntry.VerifiedCombinations, key)
				updated = true
			}
		}

		currentComb, combExists := versionEntry.VerifiedCombinations[tupleKey]

		if res.Status == "VERIFIED" {
			if !combExists || currentComb.LastTested != timestamp {
				versionEntry.VerifiedCombinations[tupleKey] = VerifiedCombination{
					Status:     res.Status,
					LastTested: timestamp,
				}
				updated = true
				fmt.Printf("Appended VERIFIED state for chip %s@%s & %s to ledger.\n", res.Chip, chipManifestVer, tupleKey)
			}
			// Prune from internal state if it exists
			if _, exists := internalState.Combinations[tupleKey]; exists {
				delete(internalState.Combinations, tupleKey)
				stateUpdated = true
			}
		} else {
			// If it's not VERIFIED, ensure it is NOT in the public ledger.
			if combExists {
				delete(versionEntry.VerifiedCombinations, tupleKey)
				updated = true
				fmt.Printf("Pruned FAILED state for chip %s@%s & %s from ledger.\n", res.Chip, chipManifestVer, tupleKey)
			}

			// Add to internal state outbox
			entry := internalState.Combinations[tupleKey]
			entry.Status = res.Status
			entry.LastTested = timestamp

			if res.Status == "INFRA_ERROR" {
				entry.RetryCount++
				if entry.RetryCount >= 2 {
					entry.Status = "FATAL_INFRA_ERROR"
					fmt.Printf("Escalated INFRA_ERROR to FATAL_INFRA_ERROR for %s\n", tupleKey)
				}
			} else {
				entry.RetryCount = 0
			}

			internalState.Combinations[tupleKey] = entry
			stateUpdated = true
		}

		if updated {
			if chipEntry.Versions == nil {
				chipEntry.Versions = make(map[string]MatrixVersion)
			}
			chipEntry.Versions[chipManifestVer] = versionEntry
			matrix[res.Chip] = chipEntry
		}
	}

	if updated {
		out, _ := json.MarshalIndent(matrix, "", "  ")
		if err := os.WriteFile("compatibility_matrix.json", out, 0644); err != nil {
			log.Fatalf("FATAL: Failed to write ledger: %v", err)
		}
		fmt.Println("compatibility_matrix.json successfully updated.")
	} else {
		fmt.Println("No new hashes to append to ledger.")
	}

	if stateUpdated {
		sOut, _ := json.MarshalIndent(internalState, "", "  ")
		if err := os.WriteFile("internal_state.json", sOut, 0644); err != nil {
			log.Printf("Warning: Failed to write internal_state.json: %v", err)
		} else {
			fmt.Println("internal_state.json successfully updated.")
		}
	}
}
