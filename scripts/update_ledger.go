package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

func GenerateComboID(prefix, chip, chipVersion, cli, core, compiler string) string {
	str := fmt.Sprintf("chip=%s@%s::cli=%s::core=%s::compiler=%s", chip, chipVersion, cli, core, compiler)
	h := crc32.ChecksumIEEE([]byte(str))
	return fmt.Sprintf("%s-%08X", prefix, h)
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
	ChipVersion     string `json:"chip_version"`
	CliVersion      string `json:"cli_version"`
	CoreVersion     string `json:"core_version"`
	CompilerVersion string `json:"compiler_version"`
	Status          string `json:"status"`
	StateHash       string `json:"state_hash"`
}

func initDB() *sql.DB {
	needsMigration := false
	if _, err := os.Stat("ledger.db"); os.IsNotExist(err) {
		needsMigration = true
	}

	db, err := sql.Open("sqlite", "ledger.db?_pragma=busy_timeout(10000)")
	if err != nil {
		log.Fatalf("FATAL: Failed to open SQLite ledger.db: %v", err)
	}

	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;

		CREATE TABLE IF NOT EXISTS verified_combinations (
			tuple_key TEXT PRIMARY KEY,
			chip TEXT,
			chip_version TEXT,
			cli_version TEXT,
			core_version TEXT,
			compiler_version TEXT,
			environment_hash TEXT,
			dependencies_json TEXT,
			status TEXT,
			last_tested TEXT
		);

		CREATE TABLE IF NOT EXISTS internal_state (
			job_id TEXT PRIMARY KEY,
			chip TEXT,
			chip_version TEXT,
			cli_version TEXT,
			core_version TEXT,
			compiler_version TEXT,
			status TEXT,
			last_tested TEXT,
			retry_count INTEGER
		);
	`)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize schema: %v", err)
	}

	if needsMigration {
		importLegacyJSON(db)
	}

	return db
}

func importLegacyJSON(db *sql.DB) {
	fmt.Println("Migrating legacy compatibility_matrix.json to ledger.db...")
	matrixData, err := os.ReadFile("compatibility_matrix.json")
	if err != nil || len(matrixData) == 0 {
		return
	}

	var matrix map[string]struct {
		Versions map[string]struct {
			EnvironmentHash string `json:"environment_hash"`
			Dependencies    Dependencies `json:"dependencies"`
			VerifiedCombinations map[string]struct {
				Status     string `json:"status"`
				LastTested string `json:"last_tested"`
			} `json:"verified_combinations"`
		} `json:"versions"`
	}

	if err := json.Unmarshal(matrixData, &matrix); err != nil {
		fmt.Printf("Warning: Failed to parse legacy matrix: %v\n", err)
		return
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT INTO verified_combinations (tuple_key, chip, chip_version, cli_version, core_version, compiler_version, environment_hash, dependencies_json, status, last_tested) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	for chip, chipData := range matrix {
		for ver, verData := range chipData.Versions {
			depsJSON, _ := json.Marshal(verData.Dependencies)
			for tupleKey, comb := range verData.VerifiedCombinations {
				parts := strings.Split(tupleKey, "::")
				var cliVer, coreVer, compVer string
				for _, p := range parts {
					if strings.HasPrefix(p, "cli=") { cliVer = strings.TrimPrefix(p, "cli=") }
					if strings.HasPrefix(p, "core=") { coreVer = strings.TrimPrefix(p, "core=") }
					if strings.HasPrefix(p, "compiler=") { compVer = strings.TrimPrefix(p, "compiler=") }
				}
				stmt.Exec(tupleKey, chip, ver, cliVer, coreVer, compVer, verData.EnvironmentHash, string(depsJSON), comb.Status, comb.LastTested)
			}
		}
	}
	tx.Commit()
	
	stateData, err := os.ReadFile("internal_state.json")
	if err == nil && len(stateData) > 0 {
		var state struct {
			Combinations map[string]struct {
				ID string `json:"id"`
				Chip string `json:"chip"`
				CliVersion string `json:"cli_version"`
				CoreVersion string `json:"core_version"`
				CompilerVersion string `json:"compiler_version"`
				Status string `json:"status"`
				LastTested string `json:"last_tested"`
				RetryCount int `json:"retry_count"`
			} `json:"combinations"`
		}
		if err := json.Unmarshal(stateData, &state); err == nil {
			tx2, _ := db.Begin()
			stmt2, _ := tx2.Prepare(`INSERT INTO internal_state (job_id, chip, cli_version, core_version, compiler_version, status, last_tested, retry_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
			for jobID, comb := range state.Combinations {
				stmt2.Exec(jobID, comb.Chip, comb.CliVersion, comb.CoreVersion, comb.CompilerVersion, comb.Status, comb.LastTested, comb.RetryCount)
			}
			tx2.Commit()
		}
	}

	fmt.Println("Legacy migration complete!")
}

func main() {
	db := initDB()
	defer db.Close()

	updated := false
	stateUpdated := false

	// One-time migration: Purge all "main" entries
	var purgeCount int
	db.QueryRow(`SELECT COUNT(*) FROM verified_combinations WHERE tuple_key LIKE '%core=main%' OR tuple_key LIKE '%cli=main%'`).Scan(&purgeCount)
	if purgeCount > 0 {
		res, _ := db.Exec("DELETE FROM verified_combinations WHERE tuple_key LIKE '%core=main%' OR tuple_key LIKE '%cli=main%'")
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected > 0 {
			fmt.Printf("Purged %d stale 'main' entries from DB.\n", rowsAffected)
			updated = true
		}
	}

	// Read all result_*.json.processing files
	matches, err := filepath.Glob("result_*.json.processing")
	if err != nil {
		log.Fatalf("FATAL: Error globbing results: %v", err)
	}

	if len(matches) == 0 && !updated {
		fmt.Println("No results found to merge.")
		return
	}

	// Read registry.json
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

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("FATAL: Failed to begin transaction: %v", err)
	}

	stmtUpsertComb, _ := tx.Prepare(`
		INSERT INTO verified_combinations (
			tuple_key, chip, chip_version, cli_version, core_version, compiler_version, 
			environment_hash, dependencies_json, status, last_tested
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tuple_key) DO UPDATE SET 
			status=excluded.status, 
			last_tested=excluded.last_tested, 
			environment_hash=excluded.environment_hash,
			dependencies_json=excluded.dependencies_json
	`)
	stmtDeleteComb, _ := tx.Prepare(`DELETE FROM verified_combinations WHERE tuple_key = ?`)
	stmtDeleteState, _ := tx.Prepare(`DELETE FROM internal_state WHERE job_id = ?`)
	stmtUpsertState, _ := tx.Prepare(`
		INSERT INTO internal_state (
			job_id, chip, chip_version, cli_version, core_version, compiler_version, status, last_tested, retry_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET 
			status=excluded.status, 
			last_tested=excluded.last_tested, 
			retry_count=excluded.retry_count
	`)
	stmtGetRetryCount, _ := tx.Prepare(`SELECT retry_count FROM internal_state WHERE job_id = ?`)

	for _, file := range matches {
		data, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Warning: Failed to read %s: %v", file, err)
			continue
		}

		var result Result
		if err := json.Unmarshal(data, &result); err != nil {
			log.Printf("Warning: Failed to parse %s: %v", file, err)
			continue
		}

		if result.Chip == "" || result.StateHash == "" || result.CliVersion == "" {
			continue
		}

		// Validate status field
		validStatuses := map[string]bool{"VERIFIED": true, "INFRA_ERROR": true, "COMPILER_ERROR": true}
		if !validStatuses[result.Status] {
			log.Printf("Warning: Skipping %s with invalid status '%s'", file, result.Status)
			continue
		}
		if result.CoreVersion == "" {
			result.CoreVersion = "main"
		}
		if result.CompilerVersion == "" {
			result.CompilerVersion = "latest"
		}

		// Use chip_version from result file (provided by the pipeline).
		// Fall back to registry lookup only for legacy results missing the field.
		chipManifestVer := result.ChipVersion
		var chipMetaDeps Dependencies

		if chipMeta, ok := reg.Chips[result.Chip]; ok {
			if chipManifestVer == "" {
				chipManifestVer = chipMeta.Version
			}
			tcName := chipMeta.CompilerPrefix
			if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
				tcName = tcName[:len(tcName)-1]
			}
			tcVer, vVer, aVer := "unknown", "unknown", "unknown"
			if tc, ok := reg.Toolchains[tcName]; ok { tcVer = tc.Version }
			if v, ok := reg.Vendors[chipMeta.Vendor]; ok { vVer = v.Version }
			if a, ok := reg.Archs[chipMeta.Arch]; ok { aVer = a.Version }

			chipMetaDeps = Dependencies{
				Toolchain: fmt.Sprintf("%s@%s", tcName, tcVer),
				Vendor:    fmt.Sprintf("%s@%s", chipMeta.Vendor, vVer),
				Arch:      fmt.Sprintf("%s@%s", chipMeta.Arch, aVer),
			}
		}
		if chipManifestVer == "" {
			chipManifestVer = "1.0.0"
		}
		depsJSON, _ := json.Marshal(chipMetaDeps)

		tupleKey := fmt.Sprintf("chip=%s@%s::cli=%s::core=%s::compiler=%s", result.Chip, chipManifestVer, result.CliVersion, result.CoreVersion, result.CompilerVersion)
		jobID := GenerateComboID("MT", result.Chip, chipManifestVer, result.CliVersion, result.CoreVersion, result.CompilerVersion)

		if result.Status == "VERIFIED" {
			_, err := stmtUpsertComb.Exec(
				tupleKey, result.Chip, chipManifestVer, result.CliVersion, result.CoreVersion, result.CompilerVersion,
				result.StateHash, string(depsJSON), result.Status, timestamp,
			)
			if err == nil {
				updated = true
				fmt.Printf("Appended VERIFIED state for chip %s@%s & %s to ledger.\n", result.Chip, chipManifestVer, tupleKey)
			}
			res, _ := stmtDeleteState.Exec(jobID)
			if aff, _ := res.RowsAffected(); aff > 0 {
				stateUpdated = true
			}
		} else {
			res, _ := stmtDeleteComb.Exec(tupleKey)
			if aff, _ := res.RowsAffected(); aff > 0 {
				updated = true
				fmt.Printf("Pruned FAILED state for chip %s@%s & %s from ledger.\n", result.Chip, chipManifestVer, tupleKey)
			}

			retryCount := 0
			err := stmtGetRetryCount.QueryRow(jobID).Scan(&retryCount)
			if err == nil && result.Status == "INFRA_ERROR" {
				retryCount++
			} else if result.Status != "INFRA_ERROR" {
				retryCount = 0
			}

			if result.Status == "INFRA_ERROR" && retryCount >= 2 {
				result.Status = "FATAL_INFRA_ERROR"
				fmt.Printf("Escalated INFRA_ERROR to FATAL_INFRA_ERROR for %s\n", jobID)
			}

			_, err = stmtUpsertState.Exec(
				jobID, result.Chip, chipManifestVer, result.CliVersion, result.CoreVersion, result.CompilerVersion,
				result.Status, timestamp, retryCount,
			)
			if err == nil {
				stateUpdated = true
			}
		}
	}

	tx.Commit()

	for _, file := range matches {
		if err := os.Remove(file); err != nil {
			log.Printf("Warning: Failed to delete processed file %s: %v", file, err)
		}
	}

	if updated {
		exportMatrix(db)
	} else {
		fmt.Println("No new hashes to append to ledger.")
	}

	if stateUpdated {
		exportInternalState(db)
	}
}

func exportMatrix(db *sql.DB) {
	rows, err := db.Query(`SELECT tuple_key, chip, chip_version, environment_hash, dependencies_json, status, last_tested FROM verified_combinations ORDER BY chip, chip_version, environment_hash`)
	if err != nil {
		log.Fatalf("FATAL: Failed to query DB for export: %v", err)
	}
	defer rows.Close()

	matrix := make(Matrix)

	// Collect rows grouped by (chip, chip_version)
	// Track the latest environment_hash per version so stale hashes don't overwrite current entries
	type versionRows struct {
		EnvHash  string
		DepsJSON string
		Combs    map[string]VerifiedCombination
	}
	versionMap := make(map[string]*versionRows) // key: "chip::chipVer"

	for rows.Next() {
		var tupleKey, chip, chipVer, envHash, depsJSON, status, lastTested string
		if err := rows.Scan(&tupleKey, &chip, &chipVer, &envHash, &depsJSON, &status, &lastTested); err != nil {
			continue
		}

		groupKey := chip + "::" + chipVer
		vr, exists := versionMap[groupKey]
		if !exists {
			vr = &versionRows{EnvHash: envHash, DepsJSON: depsJSON, Combs: make(map[string]VerifiedCombination)}
			versionMap[groupKey] = vr
		}

		// If a newer hash appears for this version, reset to only keep that hash's entries
		if vr.EnvHash != envHash {
			vr.EnvHash = envHash
			vr.DepsJSON = depsJSON
			vr.Combs = make(map[string]VerifiedCombination)
		}

		vr.Combs[tupleKey] = VerifiedCombination{Status: status, LastTested: lastTested}
	}

	// Build final matrix from grouped data
	for groupKey, vr := range versionMap {
		parts := strings.SplitN(groupKey, "::", 2)
		chip, chipVer := parts[0], parts[1]

		var deps Dependencies
		json.Unmarshal([]byte(vr.DepsJSON), &deps)

		chipEntry, exists := matrix[chip]
		if !exists {
			chipEntry = MatrixChip{Versions: make(map[string]MatrixVersion)}
			matrix[chip] = chipEntry
		}

		chipEntry.Versions[chipVer] = MatrixVersion{
			EnvironmentHash:      vr.EnvHash,
			Dependencies:         deps,
			VerifiedCombinations: vr.Combs,
		}
	}

	out, _ := json.MarshalIndent(matrix, "", "  ")
	if err := os.WriteFile("compatibility_matrix.json", out, 0644); err != nil {
		log.Fatalf("FATAL: Failed to write ledger JSON: %v", err)
	}
	fmt.Println("compatibility_matrix.json successfully updated from SQLite.")
}

func exportInternalState(db *sql.DB) {
	type InternalStateEntryJSON struct {
		ID              string `json:"id"`
		Chip            string `json:"chip"`
		ChipVersion     string `json:"chip_version"`
		CliVersion      string `json:"cli_version"`
		CoreVersion     string `json:"core_version"`
		CompilerVersion string `json:"compiler_version"`
		Status          string `json:"status"`
		LastTested      string `json:"last_tested"`
		RetryCount      int    `json:"retry_count"`
	}
	
	type InternalStateJSON struct {
		Combinations map[string]InternalStateEntryJSON `json:"combinations"`
	}

	rows, err := db.Query(`SELECT job_id, chip, COALESCE(chip_version, ''), cli_version, core_version, compiler_version, status, last_tested, retry_count FROM internal_state`)
	if err != nil {
		return
	}
	defer rows.Close()

	state := InternalStateJSON{
		Combinations: make(map[string]InternalStateEntryJSON),
	}

	for rows.Next() {
		var entry InternalStateEntryJSON
		if err := rows.Scan(&entry.ID, &entry.Chip, &entry.ChipVersion, &entry.CliVersion, &entry.CoreVersion, &entry.CompilerVersion, &entry.Status, &entry.LastTested, &entry.RetryCount); err == nil {
			state.Combinations[entry.ID] = entry
		}
	}

	out, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile("internal_state.json", out, 0644); err == nil {
		fmt.Println("internal_state.json successfully updated from SQLite.")
	}
}
