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

type MatrixChip struct {
	CurrentHardwareHash string            `json:"current_hardware_hash"`
	VerifiedCliVersions map[string]string `json:"verified_cli_versions"`
	History             []HistoryEntry    `json:"history"`
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

		chipEntry, exists := matrix[res.Chip]
		if !exists || chipEntry.CurrentHardwareHash != res.StateHash {
			// Hash changed or new chip. Reset the verified map!
			chipEntry = MatrixChip{
				CurrentHardwareHash: res.StateHash,
				VerifiedCliVersions: make(map[string]string),
				History:             []HistoryEntry{},
			}
			if exists {
				// keep old history
				chipEntry.History = matrix[res.Chip].History
			}
		}

		// Check if exact same historical run is already recorded to avoid duplicate entries
		alreadyExists := false
		for _, h := range chipEntry.History {
			if h.HardwareHash == res.StateHash && h.CliVersion == res.CliVersion && h.Status == res.Status {
				alreadyExists = true
				break
			}
		}

		if !alreadyExists {
			chipEntry.History = append(chipEntry.History, HistoryEntry{
				HardwareHash: res.StateHash,
				CliVersion:   res.CliVersion,
				Status:       res.Status,
				Timestamp:    timestamp,
			})
			
			chipEntry.VerifiedCliVersions[res.CliVersion] = res.Status
			matrix[res.Chip] = chipEntry
			updated = true
			fmt.Printf("Appended new state for chip %s @ CLI %s (Status: %s) to ledger.\n", res.Chip, res.CliVersion, res.Status)
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
