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

type VerifiedCli struct {
	Status     string `json:"status"`
	LastTested string `json:"last_tested"`
}

type MatrixVersion struct {
	EnvironmentHash     string                 `json:"environment_hash"`
	Dependencies        Dependencies           `json:"dependencies"`
	VerifiedCliVersions map[string]VerifiedCli `json:"verified_cli_versions"`
}

type MatrixChip struct {
	Versions map[string]MatrixVersion `json:"versions"`
}

type Matrix map[string]MatrixChip

type Result struct {
	Chip         string `json:"chip"`
	CliVersion   string `json:"cli_version"`
	Status       string `json:"status"`
	StateHash    string `json:"state_hash"`
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

		chipEntry, chipExists := matrix[res.Chip]
		if !chipExists {
			chipEntry = MatrixChip{
				Versions: make(map[string]MatrixVersion),
			}
			matrix[res.Chip] = chipEntry
		}

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
			Toolchains map[string]struct{ Version string `json:"version"` } `json:"toolchains"`
			Vendors    map[string]struct{ Version string `json:"version"` } `json:"vendors"`
			Archs      map[string]struct{ Version string `json:"version"` } `json:"archs"`
		}
		if err := json.Unmarshal(regData, &reg); err != nil {
			log.Fatalf("FATAL: Error parsing registry: %v", err)
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
			if tc, ok := reg.Toolchains[tcName]; ok { tcVer = tc.Version }
			
			vVer := "unknown"
			if v, ok := reg.Vendors[chipMeta.Vendor]; ok { vVer = v.Version }
			
			aVer := "unknown"
			if a, ok := reg.Archs[chipMeta.Arch]; ok { aVer = a.Version }
			
			chipMetaDeps = Dependencies{
				Toolchain: fmt.Sprintf("%s@%s", tcName, tcVer),
				Vendor:    fmt.Sprintf("%s@%s", chipMeta.Vendor, vVer),
				Arch:      fmt.Sprintf("%s@%s", chipMeta.Arch, aVer),
			}
		}

		versionEntry, versionExists := chipEntry.Versions[chipManifestVer]
		if !versionExists || versionEntry.EnvironmentHash != res.StateHash {
			versionEntry = MatrixVersion{
				EnvironmentHash:     res.StateHash,
				Dependencies:        chipMetaDeps,
				VerifiedCliVersions: make(map[string]VerifiedCli),
			}
		}

		// Update or insert current status
		currentCli, cliExists := versionEntry.VerifiedCliVersions[res.CliVersion]
		if !cliExists || currentCli.Status != res.Status {
			versionEntry.VerifiedCliVersions[res.CliVersion] = VerifiedCli{
				Status:     res.Status,
				LastTested: timestamp,
			}
			
			if chipEntry.Versions == nil {
				chipEntry.Versions = make(map[string]MatrixVersion)
			}
			chipEntry.Versions[chipManifestVer] = versionEntry
			matrix[res.Chip] = chipEntry
			updated = true
			fmt.Printf("Appended new state for chip %s@%s & CLI %s (Status: %s) to ledger.\n", res.Chip, chipManifestVer, res.CliVersion, res.Status)
		}
	}

	if updated {
		out, _ := json.MarshalIndent(matrix, "", "  ")
		if err := os.WriteFile("compatibility_matrix.json", out, 0644); err != nil {
			log.Fatalf("FATAL: Failed to write ledger: %v", err)
		}
		fmt.Println("compatibility_matrix.json successfully updated.")
	} else {
		fmt.Println("No new hashes to append.")
	}
}
