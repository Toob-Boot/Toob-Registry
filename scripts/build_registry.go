package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type ChipManifest struct {
	Name               string `json:"name"`
	Vendor             string `json:"vendor"`
	Arch               string `json:"arch"`
	CompilerPrefix     string `json:"compiler_prefix"`
	Description        string `json:"description"`
	Version            string `json:"version"`
	CoreCompatibility  string `json:"core_compatibility"`
	Path               string `json:"path,omitempty"`
	Verified           bool   `json:"verified"`
}

type ToolchainEntry struct {
	Version string            `json:"version"`
	Urls    map[string]string `json:"urls"`
	Sha256  map[string]string `json:"sha256"`
}

type VendorEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type ArchEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Registry struct {
	FormatVersion     int                       `json:"format_version"`
	RegistryVersion   string                    `json:"registry_version"`
	CoreCompatibility string                    `json:"core_compatibility"`
	Chips             map[string]ChipManifest   `json:"chips"`
	Toolchains        map[string]ToolchainEntry `json:"toolchains"`
	Vendors           map[string]VendorEntry    `json:"vendors"`
	Archs             map[string]ArchEntry      `json:"archs"`
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
		vManifest := newVendors[c.Vendor]
		aManifest := newArchs[c.Arch]
		
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

	changed := !reflect.DeepEqual(oldChips, newChips) || !reflect.DeepEqual(oldToolchains, newToolchains) || !reflect.DeepEqual(oldVendors, newVendors) || !reflect.DeepEqual(oldArchs, newArchs)

	registry.Chips = newChips
	registry.Toolchains = newToolchains
	registry.Vendors = newVendors
	registry.Archs = newArchs

	if changed {
		fmt.Println("Changes detected in chips manifests.")
		if !*validateOnly {
			oldVer := registry.RegistryVersion
			newVer := bumpPatch(oldVer)
			registry.RegistryVersion = newVer
			fmt.Printf("Bumping registry_version: %s -> %s\n", oldVer, newVer)
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
	// Append newline
	out = append(out, '\n')

	if err := os.WriteFile(regPath, out, 0644); err != nil {
		log.Fatalf("FATAL: Error writing %s: %v", regPath, err)
	}
	fmt.Println("Successfully updated registry.json")
}

func bumpPatch(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "v" + v // fallback
	}
	patch, _ := strconv.Atoi(parts[2])
	return fmt.Sprintf("v%s.%s.%d", parts[0], parts[1], patch+1)
}
