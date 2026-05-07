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

type ChipManifest struct {
	Name             string `json:"name"`
	Vendor           string `json:"vendor"`
	Arch             string `json:"arch"`
	CompilerPrefix   string `json:"compiler_prefix"`
	Description      string `json:"description"`
	Version          string `json:"version"`
	Path             string `json:"path,omitempty"`
	Verified         bool   `json:"verified"`
}

type ToolchainEntry struct {
	Path    string            `json:"path"`
	Version string            `json:"version"`
	Urls    map[string]string `json:"urls"`
	Sha256  map[string]string `json:"sha256"`
}

type VendorEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type ArchEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Version     string `json:"version"`
	Description string `json:"description"`
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
	Vendors         map[string]VendorEntry    `json:"vendors"`
	Archs           map[string]ArchEntry      `json:"archs"`
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

	oldVendors := registry.Vendors
	if oldVendors == nil {
		oldVendors = make(map[string]VendorEntry)
	}
	newVendors := make(map[string]VendorEntry)

	oldArchs := registry.Archs
	if oldArchs == nil {
		oldArchs = make(map[string]ArchEntry)
	}
	newArchs := make(map[string]ArchEntry)

	// Read Matrix
	type HistoryEntry struct {
		StateHash string `json:"state_hash"`
		Status    string `json:"status"`
	}
	type MatrixChip struct {
		History []HistoryEntry `json:"history"`
	}
	matrixData, _ := os.ReadFile("compatibility_matrix.json")
	matrix := make(map[string]MatrixChip)
	if len(matrixData) > 0 {
		_ = json.Unmarshal(matrixData, &matrix)
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

	// Walk vendors directory
	vendorDir := "vendor"
	vEntries, err := os.ReadDir(vendorDir)
	if err == nil {
		for _, entry := range vEntries {
			if !entry.IsDir() {
				continue
			}
			vName := entry.Name()
			vPath := filepath.Join(vendorDir, vName, "vendor_manifest.json")
			mdata, err := os.ReadFile(vPath)
			if err != nil {
				continue
			}
			var manifest VendorEntry
			if err := json.Unmarshal(mdata, &manifest); err != nil {
				log.Fatalf("FATAL: Error parsing %s: %v", vPath, err)
			}
			if manifest.Name == "" || manifest.Version == "" {
				log.Fatalf("FATAL: Vendor manifest '%s' is missing 'name' or 'version'", vPath)
			}
			manifest.Path = filepath.ToSlash(filepath.Join(vendorDir, vName))
			newVendors[vName] = manifest
		}
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
		if c.Name == "" || c.Vendor == "" || c.Arch == "" || c.CompilerPrefix == "" {
			log.Fatalf("FATAL: Chip '%s' is missing required fields.", chipKey)
		}

		// Verify that vendor directory actually exists in the codebase
		vendorPath := filepath.Join("vendor", c.Vendor)
		if _, err := os.Stat(vendorPath); os.IsNotExist(err) {
			log.Fatalf("FATAL: Chip '%s' declares vendor '%s', but directory '%s' does not exist in registry!", chipKey, c.Vendor, vendorPath)
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

		// Check Matrix Verification Status
		tc := newToolchains[tcName]
		vManifest, vExists := newVendors[c.Vendor]
		if !vExists {
			log.Fatalf("FATAL: Chip '%s' uses vendor '%s', but no valid vendor_manifest.json was loaded for it!", chipKey, c.Vendor)
		}
		aManifest, aExists := newArchs[c.Arch]
		if !aExists {
			log.Fatalf("FATAL: Chip '%s' uses arch '%s', but no valid arch_manifest.json was loaded for it!", chipKey, c.Arch)
		}

		cBytes, _ := json.Marshal(c)
		tBytes, _ := json.Marshal(tc)
		vBytes, _ := json.Marshal(vManifest)
		aBytes, _ := json.Marshal(aManifest)

		h := sha256.New()
		h.Write(cBytes)
		h.Write(tBytes)
		h.Write(vBytes)
		h.Write(aBytes)
		stateHash := hex.EncodeToString(h.Sum(nil))

		c.Verified = false
		if mChip, exists := matrix[chipKey]; exists {
			for _, hEntry := range mChip.History {
				if hEntry.StateHash == stateHash && hEntry.Status == "VERIFIED" {
					c.Verified = true
					break
				}
			}
		}
		newChips[chipKey] = c
	}

	registry.Chips = newChips
	registry.Toolchains = newToolchains
	registry.Vendors = newVendors
	registry.Archs = newArchs

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
			   !reflect.DeepEqual(oldVendors, newVendors) || 
			   !reflect.DeepEqual(oldArchs, newArchs)

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
