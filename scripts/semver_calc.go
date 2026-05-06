package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type VendorManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

func main() {
	// Wir vergleichen den aktuellen HEAD mit dem vorherigen Commit (HEAD^)
	out, err := exec.Command("git", "diff", "--name-status", "HEAD^", "HEAD", "vendor/").Output()
	if err != nil {
		log.Printf("No previous commit found or git diff failed: %v", err)
		os.Exit(0)
	}

	lines := strings.Split(string(out), "\n")
	changedVendors := make(map[string]struct{})

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		
		status := parts[0]
		path := parts[1]

		// vendor/esp/include/hal.h -> "esp"
		segments := strings.Split(path, "/")
		if len(segments) >= 3 && segments[0] == "vendor" {
			vendorName := segments[1]
			// We only care about C/C++ source code changes
			if strings.HasSuffix(path, ".h") || strings.HasSuffix(path, ".c") {
				evaluateVendorChange(vendorName, path, status)
				changedVendors[vendorName] = struct{}{}
			}
		}
	}

	for vendorName := range changedVendors {
		bumpVendorVersion(vendorName)
	}
}

var vendorBumps = make(map[string]int)

const (
	BUMP_NONE  = 0
	BUMP_PATCH = 1
	BUMP_MINOR = 2
	BUMP_MAJOR = 3
)

func evaluateVendorChange(vendor string, path string, status string) {
	currentBump := vendorBumps[vendor]

	// If a file is deleted, and it's a header -> MAJOR BREAK!
	if status == "D" && strings.HasSuffix(path, ".h") {
		vendorBumps[vendor] = BUMP_MAJOR
		return
	}

	if strings.HasSuffix(path, ".c") {
		if currentBump < BUMP_PATCH {
			vendorBumps[vendor] = BUMP_PATCH
		}
		return
	}

	if strings.HasSuffix(path, ".h") {
		// If a header is modified, we need to check if lines were DELETED
		out, err := exec.Command("git", "diff", "HEAD^", "HEAD", "--", path).Output()
		if err == nil {
			diffLines := strings.Split(string(out), "\n")
			for _, dl := range diffLines {
				if strings.HasPrefix(dl, "-") && !strings.HasPrefix(dl, "---") {
					// A line was deleted or modified in a Header -> Potential API Break!
					vendorBumps[vendor] = BUMP_MAJOR
					return
				}
			}
		}
		// If no lines deleted, it's just additions -> MINOR BUMP
		if currentBump < BUMP_MINOR {
			vendorBumps[vendor] = BUMP_MINOR
		}
	}
}

func bumpVendorVersion(vendor string) {
	bumpType := vendorBumps[vendor]
	if bumpType == BUMP_NONE {
		return
	}

	manifestPath := filepath.Join("vendor", vendor, "vendor_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return
	}

	var manifest VendorManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return
	}

	parts := strings.Split(manifest.Version, ".")
	if len(parts) != 3 {
		return
	}

	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])

	bumpStr := ""
	if bumpType == BUMP_MAJOR {
		major++
		minor = 0
		patch = 0
		bumpStr = "MAJOR"
	} else if bumpType == BUMP_MINOR {
		minor++
		patch = 0
		bumpStr = "MINOR"
	} else if bumpType == BUMP_PATCH {
		patch++
		bumpStr = "PATCH"
	}

	manifest.Version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	fmt.Printf("[semver_calc] Bumping vendor '%s' by %s to version %s\n", vendor, bumpStr, manifest.Version)

	newData, _ := json.MarshalIndent(manifest, "", "  ")
	// Make sure it ends with a newline to match standard formatting
	newData = append(newData, '\n')
	// We replace \u003e with > if needed, but it's fine for now
	newData = bytes.ReplaceAll(newData, []byte("\\u003e"), []byte(">"))

	os.WriteFile(manifestPath, newData, 0644)
}
