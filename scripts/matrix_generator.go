package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

type ChipManifest struct {
	Name               string `json:"name"`
	Vendor             string `json:"vendor"`
	Arch               string `json:"arch"`
	CMakeToolchainFile string `json:"cmake_toolchain_file"`
	CompilerPrefix     string `json:"compiler_prefix"`
	Description        string `json:"description"`
	Version            string `json:"version"`
	CoreCompatibility  string `json:"core_compatibility"`
	Path               string `json:"path,omitempty"`
}

type ToolchainEntry struct {
	Version string            `json:"version"`
	Urls    map[string]string `json:"urls"`
	Sha256  map[string]string `json:"sha256"`
}

type Registry struct {
	Chips      map[string]ChipManifest   `json:"chips"`
	Toolchains map[string]ToolchainEntry `json:"toolchains"`
}

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

	testQueue := []string{}

	for chipKey, chip := range registry.Chips {
		tcName := chip.CompilerPrefix
		if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
			tcName = tcName[:len(tcName)-1]
		}
		
		toolchain, exists := registry.Toolchains[tcName]
		if !exists {
			continue // Should be caught by build_registry.go anyway
		}

		// Hash chip + toolchain
		chipBytes, _ := json.Marshal(chip)
		tcBytes, _ := json.Marshal(toolchain)
		
		h := sha256.New()
		h.Write(chipBytes)
		h.Write(tcBytes)
		stateHash := hex.EncodeToString(h.Sum(nil))

		needsTest := true
		matrixEntry, exists := matrix[chipKey]
		if exists {
			for _, hEntry := range matrixEntry.History {
				if hEntry.StateHash == stateHash && hEntry.Status == "VERIFIED" {
					needsTest = false
					break
				}
			}
		}

		if needsTest {
			testQueue = append(testQueue, chipKey)
		}
	}

	out, _ := json.Marshal(testQueue)
	fmt.Println(string(out))
}
