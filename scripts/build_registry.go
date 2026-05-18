package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type ChipSources struct {
	Startup  string   `json:"startup"`
	Platform string   `json:"platform"`
	Config   string   `json:"config"`
	Linker   string   `json:"linker"`
	Hardware string   `json:"hardware"`
	Drivers  []string `json:"drivers,omitempty"`
	Extra    []string `json:"extra,omitempty"`
}

type ChipManifest struct {
	Name             string                            `json:"name"`
	Arch             string                            `json:"arch"`
	CompilerPrefix   string                            `json:"compiler_prefix"`
	Description      string                            `json:"description"`
	Version          string                            `json:"version"`
	Path             string                            `json:"path,omitempty"`
	Verified         bool                              `json:"verified"`
	Sources          *ChipSources                      `json:"sources,omitempty"`
	Includes         []string                          `json:"includes,omitempty"`
	DriverConfigs    map[string]map[string]interface{} `json:"driver_configs,omitempty"`
}

type ToolchainEntry struct {
	Path            string            `json:"path"`
	Version         string            `json:"version"`
	UpstreamVersion string            `json:"upstream_version"`
	Urls            map[string]string `json:"urls"`
	Sha256          map[string]string `json:"sha256"`
}


type ArchEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type IntegrationEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type DriverConfigField struct {
	Type        string      `json:"type"`
	Default     interface{} `json:"default"`
	Description string      `json:"description,omitempty"`
}

type DriverEntry struct {
	Name         string                       `json:"name"`
	Path         string                       `json:"path"`
	Version      string                       `json:"version"`
	Description  string                       `json:"description"`
	Category     string                       `json:"category"`
	ConfigSchema map[string]DriverConfigField `json:"config_schema,omitempty"`
}

type EcosystemIndex struct {
	Cli      []string `json:"cli"`
	CoreSDK  []string `json:"core_sdk"`
	Compiler []string `json:"compiler"`
}

type Registry struct {
	FormatVersion   int                       `json:"format_version"`
	RegistryVersion string                    `json:"registry_version"`
	Ecosystem       *EcosystemIndex           `json:"ecosystem,omitempty"`
	Chips           map[string]ChipManifest   `json:"chips"`
	Toolchains      map[string]ToolchainEntry `json:"toolchains"`
	Archs           map[string]ArchEntry          `json:"archs"`
	Drivers         map[string]DriverEntry        `json:"drivers"`
	Integrations    map[string]IntegrationEntry   `json:"integrations"`
}

func main() {
	validateOnly := flag.Bool("validate", false, "Only validate JSONs without writing (for PRs)")
	flag.Parse()

	regPath := "registry.json"
	data, err := os.ReadFile(regPath)
	if err != nil {
		log.Fatalf("FATAL: Error reading %s: %v", regPath, err)
	}

	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		log.Fatalf("FATAL: Error parsing %s: %v", regPath, err)
	}

	oldChips := registry.Chips
	if oldChips == nil {
		oldChips = make(map[string]ChipManifest)
	}
	newChips := make(map[string]ChipManifest)

	oldToolchains := registry.Toolchains
	if oldToolchains == nil {
		oldToolchains = make(map[string]ToolchainEntry)
	}
	newToolchains := make(map[string]ToolchainEntry)

	oldDrivers := registry.Drivers
	if oldDrivers == nil {
		oldDrivers = make(map[string]DriverEntry)
	}
	newDrivers := make(map[string]DriverEntry)

	oldArchs := registry.Archs
	if oldArchs == nil {
		oldArchs = make(map[string]ArchEntry)
	}
	newArchs := make(map[string]ArchEntry)

	oldIntegrations := registry.Integrations
	if oldIntegrations == nil {
		oldIntegrations = make(map[string]IntegrationEntry)
	}
	newIntegrations := make(map[string]IntegrationEntry)

	// Read Matrix
	type VerifiedCombination struct {
		Status    string `json:"status"`
	}
	type MatrixVersionEntry struct {
		EnvironmentHash      string                            `json:"environment_hash"`
		VerifiedCombinations map[string]VerifiedCombination    `json:"verified_combinations"`
	}
	type MatrixChipEntry struct {
		Versions map[string]MatrixVersionEntry `json:"versions"`
	}
	matrixData, _ := os.ReadFile("compatibility_matrix.json")
	matrix := make(map[string]MatrixChipEntry)
	if len(matrixData) > 0 {
		_ = json.Unmarshal(matrixData, &matrix)
	}

	// Read raw JSON blocks for canonical environment hashing
	var rawReg struct {
		Chips      map[string]json.RawMessage `json:"chips"`
		Toolchains map[string]json.RawMessage `json:"toolchains"`
		Archs        map[string]json.RawMessage `json:"archs"`
		Drivers      map[string]json.RawMessage `json:"drivers"`
		Integrations map[string]json.RawMessage `json:"integrations"`
	}
	json.Unmarshal(data, &rawReg)

	// Load categories
	var allowedCategories []string
	catData, err := os.ReadFile("driver_categories.json")
	if err == nil {
		json.Unmarshal(catData, &allowedCategories)
	} else {
		log.Fatalf("FATAL: Could not read driver_categories.json")
	}
	categoryMap := make(map[string]bool)
	for _, c := range allowedCategories {
		categoryMap[c] = true
	}

	// Walk toolchains directory
	tcDir := "toolchains"
	tcEntries, err := os.ReadDir(tcDir)
	if err != nil {
		log.Fatalf("FATAL: Failed to read %s directory.", tcDir)
	}

	for _, entry := range tcEntries {
		if !entry.IsDir() {
			continue
		}
		tcName := entry.Name()
		manifestPath := filepath.Join(tcDir, tcName, "toolchain_manifest.json")

		mdata, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // No manifest, skip
		}

		var manifest ToolchainEntry
		if err := json.Unmarshal(mdata, &manifest); err != nil {
			log.Fatalf("FATAL: Error parsing %s: %v", manifestPath, err)
		}
		manifest.Path = filepath.ToSlash(filepath.Join(tcDir, tcName))
		newToolchains[tcName] = manifest
	}


	// Walk archs directory
	archDir := "arch"
	aEntries, err := os.ReadDir(archDir)
	if err == nil {
		for _, entry := range aEntries {
			if !entry.IsDir() {
				continue
			}
			aName := entry.Name()
			aPath := filepath.Join(archDir, aName, "arch_manifest.json")
			mdata, err := os.ReadFile(aPath)
			if err != nil {
				continue
			}
			var manifest ArchEntry
			if err := json.Unmarshal(mdata, &manifest); err != nil {
				log.Fatalf("FATAL: Error parsing %s: %v", aPath, err)
			}
			if manifest.Name == "" || manifest.Version == "" {
				log.Fatalf("FATAL: Arch manifest '%s' is missing 'name' or 'version'", aPath)
			}
			manifest.Path = filepath.ToSlash(filepath.Join(archDir, aName))
			newArchs[aName] = manifest
		}
	}

	// Walk drivers directory recursively
	driversDir := "drivers"
	filepath.Walk(driversDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			if info.Name() == "driver_manifest.json" {
				mdata, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				var manifest DriverEntry
				if err := json.Unmarshal(mdata, &manifest); err != nil {
					log.Fatalf("FATAL: Error parsing %s: %v", path, err)
				}
				if manifest.Name == "" || manifest.Version == "" {
					log.Fatalf("FATAL: Driver manifest '%s' is missing 'name' or 'version'", path)
				}
				manifest.Path = filepath.ToSlash(filepath.Dir(path))
				
				category := filepath.Base(filepath.Dir(manifest.Path))
				if !categoryMap[category] {
					log.Fatalf("FATAL: Driver '%s' uses illegal category '%s'. Must be registered in driver_categories.json!", manifest.Name, category)
				}
				manifest.Category = category

				newDrivers[manifest.Name] = manifest
			}
		}
		return nil
	})

	// Walk integrations directory
	integrationDir := "integrations"
	iEntries, err := os.ReadDir(integrationDir)
	if err == nil {
		for _, entry := range iEntries {
			if !entry.IsDir() {
				continue
			}
			iName := entry.Name()
			iPath := filepath.Join(integrationDir, iName, "integration_manifest.json")
			mdata, err := os.ReadFile(iPath)
			if err != nil {
				continue
			}
			var manifest IntegrationEntry
			if err := json.Unmarshal(mdata, &manifest); err != nil {
				log.Fatalf("FATAL: Error parsing %s: %v", iPath, err)
			}
			if manifest.Name == "" || manifest.Version == "" {
				log.Fatalf("FATAL: Integration manifest '%s' is missing 'name' or 'version'", iPath)
			}
			manifest.Path = filepath.ToSlash(filepath.Join(integrationDir, iName))
			newIntegrations[iName] = manifest
		}
	}

	// Walk chips directory
	chipsDir := "chips"
	entries, err := os.ReadDir(chipsDir)
	if err != nil {
		log.Fatalf("FATAL: Failed to read %s directory. Ensure you are running this from the registry root.", chipsDir)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chipName := entry.Name()
		manifestPath := filepath.Join(chipsDir, chipName, "chip_manifest.json")

		mdata, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // No manifest, skip
		}

		var manifest ChipManifest
		if err := json.Unmarshal(mdata, &manifest); err != nil {
			log.Fatalf("FATAL: Error parsing %s: %v", manifestPath, err)
		}

		// Auto-inject path
		manifest.Path = filepath.ToSlash(filepath.Join(chipsDir, chipName))
		newChips[chipName] = manifest
	}

	// Strict Integrity Checks
	if len(newToolchains) == 0 {
		log.Fatalf("FATAL: Missing toolchains in registry (no toolchain_manifest.json found)")
	}

	// Crypto-Validation for Toolchains
	requiredArchs := []string{"linux_amd64", "windows_amd64", "darwin_amd64"}
	for tcName, tcData := range newToolchains {
		for _, arch := range requiredArchs {
			hash, exists := tcData.Sha256[arch]
			if !exists || len(hash) != 64 {
				log.Fatalf("FATAL: Toolchain '%s' is missing a valid 64-character SHA256 hash for architecture '%s'.", tcName, arch)
			}
		}
	}

	// Validate chips
	for chipKey, c := range newChips {
		if c.Name == "" || c.Arch == "" || c.CompilerPrefix == "" {
			log.Fatalf("FATAL: Chip '%s' is missing required fields.", chipKey)
		}

		// Validate sources (Flat BOM)
		if c.Sources == nil {
			log.Fatalf("FATAL: Chip '%s' is missing the 'sources' block (Flat BOM requirement)!", chipKey)
		}

		chipDir := filepath.Join("chips", chipKey)
		validateSourceFile := func(name, path string) {
			if path == "" {
				log.Fatalf("FATAL: Chip '%s' sources block is missing '%s'!", chipKey, name)
			}
			// Paths that do not start with a specific registry root folder are assumed to be chip-local
			fullPath := filepath.Join(chipDir, path)
			if strings.HasPrefix(path, "drivers/") || strings.HasPrefix(path, "arch/") {
				fullPath = path
			}
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				log.Fatalf("FATAL: Chip '%s' declares source '%s' at '%s', but file does not exist!", chipKey, name, fullPath)
			}
		}

		validateSourceFile("startup", c.Sources.Startup)
		validateSourceFile("platform", c.Sources.Platform)
		validateSourceFile("config", c.Sources.Config)
		validateSourceFile("linker", c.Sources.Linker)
		validateSourceFile("hardware", c.Sources.Hardware)

		for i, drv := range c.Sources.Drivers {
			if _, err := os.Stat(drv); os.IsNotExist(err) {
				log.Fatalf("FATAL: Chip '%s' declares driver[%d] '%s', but file does not exist in registry!", chipKey, i, drv)
			}
		}

		for i, ext := range c.Sources.Extra {
			validateSourceFile(fmt.Sprintf("extra[%d]", i), ext)
		}

		// Verify that arch directory actually exists in the codebase
		archPath := filepath.Join("arch", c.Arch)
		if _, err := os.Stat(archPath); os.IsNotExist(err) {
			log.Fatalf("FATAL: Chip '%s' declares arch '%s', but directory '%s' does not exist in registry!", chipKey, c.Arch, archPath)
		}

		// Verify that the compiler_prefix actually exists in the toolchains dictionary!
		tcName := strings.TrimSuffix(c.CompilerPrefix, "-")
		if _, exists := newToolchains[tcName]; !exists {
			log.Fatalf("FATAL: Chip '%s' references compiler_prefix '%s' (toolchain '%s'), but it is NOT defined in any toolchain_manifest.json!", chipKey, c.CompilerPrefix, tcName)
		}

		if c.DriverConfigs != nil {
			for drvName, drvConf := range c.DriverConfigs {
				drvManifest, exists := newDrivers[drvName]
				if !exists {
					log.Fatalf("FATAL: Chip '%s' configures driver '%s', but this driver does not exist!", chipKey, drvName)
				}
				if drvManifest.ConfigSchema == nil {
					log.Fatalf("FATAL: Chip '%s' configures driver '%s', but driver has no config_schema!", chipKey, drvName)
				}
				for key, val := range drvConf {
					schemaField, hasField := drvManifest.ConfigSchema[key]
					if !hasField {
						log.Fatalf("FATAL: Chip '%s' sets config '%s' for driver '%s', but it is not in the config_schema!", chipKey, key, drvName)
					}
					if schemaField.Type == "int" {
						if _, ok := val.(float64); !ok {
							log.Fatalf("FATAL: Chip '%s' driver '%s' config '%s' must be an integer!", chipKey, drvName, key)
						}
					} else if schemaField.Type == "bool" {
						if _, ok := val.(bool); !ok {
							log.Fatalf("FATAL: Chip '%s' driver '%s' config '%s' must be a boolean!", chipKey, drvName, key)
						}
					} else if schemaField.Type == "string" || schemaField.Type == "hex" {
						if _, ok := val.(string); !ok {
							log.Fatalf("FATAL: Chip '%s' driver '%s' config '%s' must be a string/hex!", chipKey, drvName, key)
						}
					}
				}
			}
		}

		// Check Matrix Verification Status

		if _, aExists := newArchs[c.Arch]; !aExists {
			log.Fatalf("FATAL: Chip '%s' uses arch '%s', but no valid arch_manifest.json was loaded for it!", chipKey, c.Arch)
		}

		// Canonical environment hash using raw JSON blocks from registry.json
		h := sha256.New()
		if raw, ok := rawReg.Chips[chipKey]; ok {
			h.Write(raw)
		}
		if raw, ok := rawReg.Toolchains[tcName]; ok {
			h.Write(raw)
		}
		if raw, ok := rawReg.Archs[c.Arch]; ok {
			h.Write(raw)
		}
		
		for _, drvPath := range c.Sources.Drivers {
			drvName := filepath.Base(filepath.Dir(drvPath))
			if raw, ok := rawReg.Drivers[drvName]; ok {
				h.Write(raw)
			}
		}
		
		for _, raw := range rawReg.Integrations {
			h.Write(raw)
		}

		stateHash := hex.EncodeToString(h.Sum(nil))

		c.Verified = false
		if mChip, exists := matrix[chipKey]; exists {
			chipVer := c.Version
			if chipVer == "" {
				chipVer = "1.0.0"
			}
			if verEntry, verExists := mChip.Versions[chipVer]; verExists {
				if verEntry.EnvironmentHash == stateHash {
					// All combinations under this version share the same env hash;
					// if any is VERIFIED, the chip environment is verified.
					for _, comb := range verEntry.VerifiedCombinations {
						if comb.Status == "VERIFIED" {
							c.Verified = true
							break
						}
					}
				}
			}
		}
		newChips[chipKey] = c
	}

	registry.Chips = newChips
	registry.Toolchains = newToolchains
	registry.Archs = newArchs
	registry.Drivers = newDrivers
	registry.Integrations = newIntegrations

	// Read Releases Index
	releasesData, err := os.ReadFile("releases.json")
	var newEcosystem EcosystemIndex
	if err == nil {
		json.Unmarshal(releasesData, &newEcosystem)
		// Wait to assign until after comparison
		os.Remove("releases.json")
	} else {
		// If no new releases.json, keep the old one
	}
	
	// Has anything changed?
	changed := !reflect.DeepEqual(oldChips, newChips) || 
	           !reflect.DeepEqual(oldToolchains, newToolchains) || 
			   !reflect.DeepEqual(oldArchs, newArchs) ||
			   !reflect.DeepEqual(oldIntegrations, newIntegrations)

	if err == nil {
		// Only consider releases changed if we actually read a new releases.json
		// Compare with old registry.Releases if it exists. If it doesn't, it's a change.
		// We'll just do a simple string comparison of the marshalled JSONs
		oldRelJSON, _ := json.Marshal(registry.Ecosystem)
		newRelJSON, _ := json.Marshal(newEcosystem)
		if string(oldRelJSON) != string(newRelJSON) {
			changed = true
		}
		registry.Ecosystem = &newEcosystem
	}

	if changed {
		fmt.Println("Changes detected in manifests or ecosystem releases.")
		if !*validateOnly {
			// Read max bump from chips if available
			maxBump := 1 // Default to PATCH
			bumpData, err := os.ReadFile(".chip_bumps.json")
			if err == nil {
				os.Remove(".chip_bumps.json")
				var chipBumps map[string]int
				json.Unmarshal(bumpData, &chipBumps)
				for _, b := range chipBumps {
					if b > maxBump {
						maxBump = b
					}
				}
			}

			oldVer := registry.RegistryVersion
			newVer := bumpVersion(oldVer, maxBump)
			registry.RegistryVersion = newVer
			
			bumpName := "PATCH"
			if maxBump == 2 { bumpName = "MINOR" }
			if maxBump == 3 { bumpName = "MAJOR" }
			fmt.Printf("Bumping registry_version (%s): %s -> %s\n", bumpName, oldVer, newVer)
		}
	} else {
		fmt.Println("No changes detected.")
	}

	if *validateOnly {
		fmt.Println("Validation passed successfully! All JSON manifests and cryptography checks are structurally sound.")
		os.Exit(0)
	}

	// Write back deterministically
	out, err := json.MarshalIndent(registry, "", "    ")
	if err != nil {
		log.Fatalf("FATAL: Error marshaling registry: %v", err)
	}
	out = bytes.ReplaceAll(out, []byte("\\u003e"), []byte(">"))
	out = bytes.ReplaceAll(out, []byte("\\u003c"), []byte("<"))
	// Append newline
	out = append(out, '\n')

	if err := os.WriteFile(regPath, out, 0644); err != nil {
		log.Fatalf("FATAL: Error writing %s: %v", regPath, err)
	}
	fmt.Println("Successfully updated registry.json")
}

func bumpVersion(v string, bumpType int) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "v" + v // fallback
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])
	
	if bumpType == 3 { // MAJOR
		major++
		minor = 0
		patch = 0
	} else if bumpType == 2 { // MINOR
		minor++
		patch = 0
	} else { // PATCH (or default)
		patch++
	}
	
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch)
}
