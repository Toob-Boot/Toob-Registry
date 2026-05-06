package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type HistoryEntry struct {
	CliVersion   string `json:"cli_version"`
	HardwareHash string `json:"hardware_hash"`
	Status       string `json:"status"`
	Timestamp    string `json:"timestamp"`
}

type MatrixVersion struct {
	HardwareHash        string            `json:"hardware_hash"`
	VerifiedCliVersions map[string]string `json:"verified_cli_versions"`
	History             []HistoryEntry    `json:"history"`
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
				Version string `json:"version"`
			} `json:"chips"`
		}
		if err := json.Unmarshal(regData, &reg); err != nil {
			log.Fatalf("FATAL: Error parsing registry: %v", err)
		}

		chipManifestVer := "1.0.0"
		if chipMeta, ok := reg.Chips[res.Chip]; ok {
			chipManifestVer = chipMeta.Version
		}

		versionEntry, versionExists := chipEntry.Versions[chipManifestVer]
		if !versionExists || versionEntry.HardwareHash != res.StateHash {
			versionEntry = MatrixVersion{
				HardwareHash:        res.StateHash,
				VerifiedCliVersions: make(map[string]string),
				History:             []HistoryEntry{},
			}
			if versionExists {
				versionEntry.History = chipEntry.Versions[chipManifestVer].History
			}
		}

		alreadyExists := false
		for _, h := range versionEntry.History {
			if h.HardwareHash == res.StateHash && h.CliVersion == res.CliVersion && h.Status == res.Status {
				alreadyExists = true
				break
			}
		}

		if !alreadyExists {
			versionEntry.History = append(versionEntry.History, HistoryEntry{
				HardwareHash: res.StateHash,
				CliVersion:   res.CliVersion,
				Status:       res.Status,
				Timestamp:    timestamp,
			})
			
			versionEntry.VerifiedCliVersions[res.CliVersion] = res.Status
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
