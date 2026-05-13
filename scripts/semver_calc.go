package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

/*
================================================================================
  SEMVER HARDWARE GUIDELINES (FOR LLMs & DEVELOPERS)
================================================================================
When updating code in vendor/, arch/, or toolchains/, you MUST manually bump the
version in the respective manifest (e.g. vendor_manifest.json).

Use the following semantic versioning rules for HARDWARE definitions:

- PATCH (x.x.+1): Bugfixes that DO NOT affect the ABI (Application Binary Interface)
                  or memory layout. Example: fixing a logic bug inside a C function,
                  fixing a typo in a macro, updating a comment.

- MINOR (x.+1.x): Adding new features that are BACKWARDS COMPATIBLE.
                  Example: adding a new C function to the HAL, adding a new
                  peripheral driver that doesn't change existing structs.

- MAJOR (+1.x.x): BREAKING CHANGES. Any change that alters the ABI, memory map,
                  or struct sizes. Example: adding a field to an existing struct
                  (changes sizeof!), modifying linker script addresses, updating
                  the underlying GCC compiler version.

This script (semver_calc.go) will automatically calculate the difference between
the old version and your new version, and VERERBT (inherits) the HIGHEST bump type
to all dependent Chips.
================================================================================
*/

const (
	BumpNone  = 0
	BumpPatch = 1
	BumpMinor = 2
	BumpMajor = 3
)

type ChipManifest struct {
	Vendor         string `json:"vendor"`
	Arch           string `json:"arch"`
	CompilerPrefix string `json:"compiler_prefix"`
	Version        string `json:"version"`
}

// readFileFromGit reads a raw file from a specific git commit
func readFileFromGit(sha, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", sha, path))
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// readVersionFromGit reads a JSON manifest and extracts the "version" string
func readVersionFromGit(sha, path string) (string, error) {
	data, err := readFileFromGit(sha, path)
	if err != nil {
		return "", err
	}

	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	return manifest.Version, nil
}

// checkFolderChanges checks if significant code files changed in a folder
func checkFolderChanges(shaBefore, shaCurrent, folder string) bool {
	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s..%s", shaBefore, shaCurrent), folder)
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		ext := filepath.Ext(line)
		if ext == ".c" || ext == ".h" || ext == ".ld" || ext == ".S" || ext == ".cmake" || ext == ".json" {
			return true
		}
	}
	return false
}

// getAutoBumpType reads commit history to determine bump type
func getAutoBumpType(shaBefore, shaCurrent, folder string) int {
	cmd := exec.Command("git", "log", "--oneline", fmt.Sprintf("%s..%s", shaBefore, shaCurrent), folder)
	out, err := cmd.Output()
	if err != nil {
		return BumpPatch
	}
	logOutput := string(out)
	if strings.Contains(logOutput, "BREAKING CHANGE:") || strings.Contains(logOutput, "feat!:") || strings.Contains(logOutput, "fix!:") {
		return BumpMajor
	}
	if strings.Contains(logOutput, "feat:") {
		return BumpMinor
	}
	return BumpPatch
}

// calcBumpType compares two semver strings and returns the bump type
func calcBumpType(oldVer, newVer string) int {
	if oldVer == newVer {
		return BumpNone
	}
	if oldVer == "" && newVer != "" {
		return BumpMinor // New dependency added
	}
	if oldVer != "" && newVer == "" {
		return BumpMajor // Dependency deleted
	}

	oMajor, oMinor, oPatch := parseSemVer(oldVer)
	nMajor, nMinor, nPatch := parseSemVer(newVer)

	// Downgrade protection
	if nMajor < oMajor || (nMajor == oMajor && nMinor < oMinor) || (nMajor == oMajor && nMinor == oMinor && nPatch < oPatch) {
		log.Fatalf("FATAL: Version downgrade detected! %s -> %s is not allowed.", oldVer, newVer)
	}

	if nMajor > oMajor {
		return BumpMajor
	}
	if nMinor > oMinor && nMajor == oMajor {
		return BumpMinor
	}
	if nPatch > oPatch && nMajor == oMajor && nMinor == oMinor {
		return BumpPatch
	}

	// Gap 7: If versions are different (e.g. date suffix changed) but major/minor/patch are the same
	if oldVer != newVer {
		return BumpPatch
	}

	return BumpNone
}

func parseSemVer(v string) (major, minor, patch int) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return
}

func formatSemVer(major, minor, patch int) string {
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func applyBump(ver string, bumpType int) string {
	major, minor, patch := parseSemVer(ver)
	if bumpType == BumpMajor {
		major++
		minor = 0
		patch = 0
	} else if bumpType == BumpMinor {
		minor++
		patch = 0
	} else if bumpType == BumpPatch {
		patch++
	}
	return formatSemVer(major, minor, patch)
}

func getBumpTypeName(bumpType int) string {
	switch bumpType {
	case BumpMajor:
		return "MAJOR"
	case BumpMinor:
		return "MINOR"
	case BumpPatch:
		return "PATCH"
	default:
		return "NONE"
	}
}

// Fallback logic for Force-Pushes
func resolveBeforeSha(sha string) string {
	if sha == "0000000000000000000000000000000000000000" || sha == "" {
		return "HEAD^"
	}

	// Test if sha exists in history
	cmd := exec.Command("git", "cat-file", "-t", sha)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: BEFORE_SHA %s not found in local git history (Force-Push?). Falling back to latest tag.\n", sha)
		cmdTag := exec.Command("git", "describe", "--tags", "--abbrev=0")
		tagOut, errTag := cmdTag.Output()
		if errTag == nil {
			return strings.TrimSpace(string(tagOut))
		}
		return "HEAD^" // Absolute fallback
	}
	return sha
}

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Usage: go run semver_calc.go <BEFORE_SHA> <CURRENT_SHA>")
	}
	beforeSha := resolveBeforeSha(os.Args[1])
	currentSha := os.Args[2]

	fmt.Printf("Running Toob SemVer & Dependency Resolver (%s -> %s)...\n", beforeSha, currentSha)

	changesMade := false
	dependencyBumps := make(map[string]int)

	// Helper to check and record a dependency's bump
	checkDep := func(category, name string) {
		path := fmt.Sprintf("%s/%s/%s_manifest.json", category, name, strings.TrimSuffix(category, "s"))
		folder := fmt.Sprintf("%s/%s", category, name)

		if category == "vendor" {
			path = fmt.Sprintf("vendor/%s/vendor_manifest.json", name)
		} else if category == "arch" {
			path = fmt.Sprintf("arch/%s/arch_manifest.json", name)
		} else if category == "toolchains" {
			path = fmt.Sprintf("toolchains/%s/toolchain_manifest.json", name)
		}

		oldVer, _ := readVersionFromGit(beforeSha, path)
		newVer, _ := readVersionFromGit(currentSha, path)

		bump := calcBumpType(oldVer, newVer)

		// The Human Error Fix: Code changed but version didn't
		if bump == BumpNone && oldVer != "" {
			if checkFolderChanges(beforeSha, currentSha, folder) {
				bump = getAutoBumpType(beforeSha, currentSha, folder)
				newVer = applyBump(oldVer, bump)
				fmt.Printf("[Auto-Fix] Code changed in %s but version not bumped. Auto-bumping %s -> %s (%s)\n", folder, oldVer, newVer, getBumpTypeName(bump))

				manifestPath := path
				mBytes, err := os.ReadFile(manifestPath)
				if err == nil {
					content := string(mBytes)
					re := regexp.MustCompile(`"version"\s*:\s*"[^"]+"`)
					replaced := false
					newContent := re.ReplaceAllStringFunc(content, func(match string) string {
						if !replaced {
							replaced = true
							return fmt.Sprintf(`"version": "%s"`, newVer)
						}
						return match
					})
					if os.Getenv("GITHUB_EVENT_NAME") != "pull_request" {
						os.WriteFile(manifestPath, []byte(newContent), 0644)
					} else {
						fmt.Printf("[Dry-Run] Would have updated %s\n", manifestPath)
					}
					changesMade = true
				}
			}
		}

		if bump > BumpNone {
			dependencyBumps[path] = bump
			if oldVer != newVer {
				fmt.Printf("[Detected] %s updated: %s -> %s (%s)\n", path, oldVer, newVer, getBumpTypeName(bump))
			}
		}
	}

	// Read CURRENT registry state to know what exists
	regData, err := os.ReadFile("registry.json")
	if err != nil {
		log.Fatalf("FATAL: Cannot run semver resolver without built registry.json")
	}

	var reg struct {
		Chips      map[string]ChipManifest `json:"chips"`
		Toolchains map[string]interface{}  `json:"toolchains"`
		Vendors    map[string]interface{}  `json:"vendors"`
		Archs      map[string]interface{}  `json:"archs"`
	}
	json.Unmarshal(regData, &reg)

	// Check all dependencies
	for v := range reg.Vendors {
		checkDep("vendor", v)
	}
	for a := range reg.Archs {
		checkDep("arch", a)
	}
	for tc := range reg.Toolchains {
		checkDep("toolchains", tc)
	}

	// Gap 6: Read chips directly from the directory to detect completely new chips
	chipBumps := make(map[string]int)
	chipsDir := "chips"
	entries, err := os.ReadDir(chipsDir)
	if err != nil {
		log.Fatalf("FATAL: Failed to read %s directory", chipsDir)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chipKey := entry.Name()
		
		// Fallback values in case it's a completely new chip without registry entry yet
		vendor, arch, compilerPrefix := "", "", ""
		cPath := fmt.Sprintf("chips/%s/chip_manifest.json", chipKey)
		
		// Read current local manifest to get its dependencies
		localData, err := os.ReadFile(cPath)
		if err == nil {
			var localManifest ChipManifest
			if json.Unmarshal(localData, &localManifest) == nil {
				vendor = localManifest.Vendor
				arch = localManifest.Arch
				compilerPrefix = localManifest.CompilerPrefix
			}
		}

		tcName := compilerPrefix
		if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
			tcName = tcName[:len(tcName)-1]
		}

		folder := fmt.Sprintf("chips/%s", chipKey)
		vPath := fmt.Sprintf("vendor/%s/vendor_manifest.json", vendor)
		aPath := fmt.Sprintf("arch/%s/arch_manifest.json", arch)
		tcPath := fmt.Sprintf("toolchains/%s/toolchain_manifest.json", tcName)

		oldChipVer, _ := readVersionFromGit(beforeSha, cPath)
		newChipVer, _ := readVersionFromGit(currentSha, cPath)
		ownBump := calcBumpType(oldChipVer, newChipVer)

		// The Human Error Fix for Chips
		if ownBump == BumpNone && oldChipVer != "" {
			if checkFolderChanges(beforeSha, currentSha, folder) {
				ownBump = getAutoBumpType(beforeSha, currentSha, folder)
				newChipVer = applyBump(oldChipVer, ownBump)
				fmt.Printf("[Auto-Fix] Code changed in %s but version not bumped. Auto-bumping %s -> %s (%s)\n", folder, oldChipVer, newChipVer, getBumpTypeName(ownBump))

				manifestPath := filepath.Join("chips", chipKey, "chip_manifest.json")
				mBytes, err := os.ReadFile(manifestPath)
				if err == nil {
					content := string(mBytes)
					re := regexp.MustCompile(`"version"\s*:\s*"[^"]+"`)
					replaced := false
					newContent := re.ReplaceAllStringFunc(content, func(match string) string {
						if !replaced {
							replaced = true
							return fmt.Sprintf(`"version": "%s"`, newChipVer)
						}
						return match
					})
					if os.Getenv("GITHUB_EVENT_NAME") != "pull_request" {
						os.WriteFile(manifestPath, []byte(newContent), 0644)
					} else {
						fmt.Printf("[Dry-Run] Would have updated %s\n", manifestPath)
					}
					changesMade = true
				}
			}
		}

		if ownBump > BumpNone {
			fmt.Printf("[Detected] Chip '%s' own code updated: %s -> %s (%s)\n", chipKey, oldChipVer, newChipVer, getBumpTypeName(ownBump))
		}

		// Calculate max inherited bump
		maxBump := ownBump
		if b := dependencyBumps[vPath]; b > maxBump {
			maxBump = b
		}
		if b := dependencyBumps[aPath]; b > maxBump {
			maxBump = b
		}
		if b := dependencyBumps[tcPath]; b > maxBump {
			maxBump = b
		}

		if maxBump > BumpNone {
			// If maxBump is greater than the chip's own bump, we must force a bump on the chip!
			// If maxBump == ownBump, the developer already bumped it correctly manually.
			if maxBump > ownBump {
				fmt.Printf("[Inheritance] Chip '%s' (v%s) inherits %s bump from dependencies!\n", chipKey, newChipVer, getBumpTypeName(maxBump))

				newVersion := applyBump(newChipVer, maxBump)
				fmt.Printf(" -> Auto-Bumping '%s' to v%s\n", chipKey, newVersion)

				manifestPath := filepath.Join("chips", chipKey, "chip_manifest.json")
				mBytes, err := os.ReadFile(manifestPath)
				if err == nil {
					content := string(mBytes)
					re := regexp.MustCompile(`"version"\s*:\s*"[^"]+"`)
					replaced := false
					newContent := re.ReplaceAllStringFunc(content, func(match string) string {
						if !replaced {
							replaced = true
							return fmt.Sprintf(`"version": "%s"`, newVersion)
						}
						return match
					})
					if os.Getenv("GITHUB_EVENT_NAME") != "pull_request" {
						os.WriteFile(manifestPath, []byte(newContent), 0644)
					} else {
						fmt.Printf("[Dry-Run] Would have updated %s\n", manifestPath)
					}
					changesMade = true
				}
				chipBumps[chipKey] = maxBump
			} else {
				chipBumps[chipKey] = ownBump
			}
		}
	}

	// "The Silent Breaking Change" Fix: Detect deleted chips
	oldRegData, err := readFileFromGit(beforeSha, "registry.json")
	if err == nil {
		var oldReg struct {
			Chips map[string]interface{} `json:"chips"`
		}
		if json.Unmarshal(oldRegData, &oldReg) == nil {
			for oldChipKey := range oldReg.Chips {
				if _, exists := reg.Chips[oldChipKey]; !exists {
					fmt.Printf("[FATAL CHANGE] Chip '%s' was deleted! This triggers a MAJOR registry bump.\n", oldChipKey)
					chipBumps["__DELETED_CHIP__"] = BumpMajor
				}
			}
		}
	}

	// Save chip bumps to a temporary file so build_registry.go can inherit them
	if len(chipBumps) > 0 {
		bumpData, _ := json.Marshal(chipBumps)
		os.WriteFile(".chip_bumps.json", bumpData, 0644)
	} else {
		os.Remove(".chip_bumps.json")
	}

	if changesMade {
		fmt.Println("Successfully resolved and auto-bumped SemVer manifests.")
	} else {
		fmt.Println("All SemVer manifests are up to date.")
	}
}
