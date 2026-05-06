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
	StateHash string `json:"state_hash"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type MatrixChip struct {
	CurrentHash string         `json:"current_hash"`
	History     []HistoryEntry `json:"history"`
}

type Matrix map[string]MatrixChip

type Result struct {
	Chip      string `json:"chip"`
	Status    string `json:"status"`
	StateHash string `json:"state_hash"`
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

		if res.Chip == "" || res.StateHash == "" {
			continue
		}

		chipEntry, exists := matrix[res.Chip]
		if !exists {
			chipEntry = MatrixChip{
				History: []HistoryEntry{},
			}
		}

		// Check if hash already in history
		alreadyExists := false
		for _, h := range chipEntry.History {
			if h.StateHash == res.StateHash && h.Status == res.Status {
				alreadyExists = true
				break
			}
		}

		if !alreadyExists {
			chipEntry.History = append(chipEntry.History, HistoryEntry{
				StateHash: res.StateHash,
				Status:    res.Status,
				Timestamp: timestamp,
			})
			chipEntry.CurrentHash = res.StateHash
			matrix[res.Chip] = chipEntry
			updated = true
			fmt.Printf("Appended new state for chip %s (Hash: %s) to ledger.\n", res.Chip, res.StateHash)
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
