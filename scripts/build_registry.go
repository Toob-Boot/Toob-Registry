package main

import (
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
	FormatVersion     int                       `json:"format_version"`
	RegistryVersion   string                    `json:"registry_version"`
	CoreCompatibility string                    `json:"core_compatibility"`
	Chips             map[string]ChipManifest   `json:"chips"`
	Toolchains        map[string]ToolchainEntry `json:"toolchains"`
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
		if c.Name == "" || c.Vendor == "" || c.Arch == "" || c.CMakeToolchainFile == "" || c.CompilerPrefix == "" {
			log.Fatalf("FATAL: Chip '%s' is missing required fields.", chipKey)
		}

		// Verify that the compiler_prefix actually exists in the toolchains dictionary!
		tcName := strings.TrimSuffix(c.CompilerPrefix, "-")
		if _, exists := newToolchains[tcName]; !exists {
			log.Fatalf("FATAL: Chip '%s' references compiler_prefix '%s' (toolchain '%s'), but it is NOT defined in any toolchain_manifest.json!", chipKey, c.CompilerPrefix, tcName)
		}
	}

	changed := !reflect.DeepEqual(oldChips, newChips) || !reflect.DeepEqual(oldToolchains, newToolchains)

	registry.Chips = newChips
	registry.Toolchains = newToolchains

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
