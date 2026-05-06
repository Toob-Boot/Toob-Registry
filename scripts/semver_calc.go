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

type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type ChipManifest struct {
	Name           string `json:"name"`
	Vendor         string `json:"vendor"`
	Arch           string `json:"arch"`
	CompilerPrefix string `json:"compiler_prefix"`
	Version        string `json:"version"`
}

const (
	BUMP_NONE  = 0
	BUMP_PATCH = 1
	BUMP_MINOR = 2
	BUMP_MAJOR = 3
)

var componentBumps = make(map[string]int) // e.g. "vendor/esp" -> BUMP_MAJOR
var manualOverride int = BUMP_NONE

func main() {
	if len(os.Args) < 3 {
		log.Printf("Usage: semver_calc <before_sha> <current_sha>")
		// Fallback to HEAD^ HEAD for local testing
		os.Args = []string{os.Args[0], "HEAD^", "HEAD"}
	}
	beforeSha := os.Args[1]
	currentSha := os.Args[2]

	checkManualOverrides(beforeSha, currentSha)

	// Get changed files
	out, err := exec.Command("git", "diff", "--name-status", beforeSha, currentSha).Output()
	if err != nil {
		log.Printf("Git diff failed: %v", err)
		os.Exit(0)
	}

	lines := strings.Split(string(out), "\n")

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

		segments := strings.Split(path, "/")
		if len(segments) >= 3 && (segments[0] == "vendor" || segments[0] == "arch" || segments[0] == "chips") {
			compPath := segments[0] + "/" + segments[1]
			
			if manualOverride != BUMP_NONE {
				setBump(compPath, manualOverride)
				continue
			}

			if strings.HasSuffix(path, ".h") || strings.HasSuffix(path, ".c") {
				evaluateCFile(compPath, path, status, beforeSha, currentSha)
			} else if strings.HasSuffix(path, ".json") {
				// E.g. changing hardware.json
				setBump(compPath, BUMP_PATCH)
			}
		}
	}

	cascadeBumps()

	for compPath, bumpType := range componentBumps {
		applyBump(compPath, bumpType)
	}
}

func checkManualOverrides(before, current string) {
	out, err := exec.Command("git", "log", "--format=%B", before+".."+current).Output()
	if err != nil {
		return
	}
	msg := string(out)
	if strings.Contains(msg, "BREAKING CHANGE:") || strings.Contains(msg, "chore: [major]") || strings.Contains(msg, "feat!: ") {
		manualOverride = BUMP_MAJOR
	} else if strings.Contains(msg, "chore: [minor]") {
		manualOverride = BUMP_MINOR
	} else if strings.Contains(msg, "chore: [patch]") {
		manualOverride = BUMP_PATCH
	}
}

func setBump(comp string, bump int) {
	if componentBumps[comp] < bump {
		componentBumps[comp] = bump
	}
}

func evaluateCFile(comp string, path string, status string, before string, current string) {
	if status == "D" && strings.HasSuffix(path, ".h") {
		setBump(comp, BUMP_MAJOR)
		return
	}

	if strings.HasSuffix(path, ".c") {
		setBump(comp, BUMP_PATCH)
		return
	}

	if strings.HasSuffix(path, ".h") {
		oldContent, errOld := exec.Command("git", "show", before+":"+path).Output()
		newContent, errNew := exec.Command("git", "show", current+":"+path).Output()
		
		if errOld != nil || errNew != nil {
			setBump(comp, BUMP_MINOR) // likely a new file
			return
		}

		oldClean := stripCommentsAndWhitespace(string(oldContent))
		newClean := stripCommentsAndWhitespace(string(newContent))

		if oldClean == newClean {
			setBump(comp, BUMP_PATCH) // Only comments/whitespace changed
			return
		}

		// Simple diff to check for deleted clean lines
		if hasDeletedLines(oldClean, newClean) {
			setBump(comp, BUMP_MAJOR)
			return
		}
		
		// If struct or enum is added/modified, it can shift memory layout
		if strings.Contains(newClean, "struct") || strings.Contains(newClean, "enum") {
			setBump(comp, BUMP_MAJOR)
			return
		}

		setBump(comp, BUMP_MINOR)
	}
}

func stripCommentsAndWhitespace(code string) string {
	// Strip block comments
	reBlock := regexp.MustCompile(`(?s)/\*.*?\*/`)
	code = reBlock.ReplaceAllString(code, "")
	
	// Strip line comments
	reLine := regexp.MustCompile(`//.*`)
	code = reLine.ReplaceAllString(code, "")
	
	// Strip empty lines
	var clean []string
	for _, line := range strings.Split(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, "\n")
}

func hasDeletedLines(old, new string) bool {
	oldLines := strings.Split(old, "\n")
	newMap := make(map[string]bool)
	for _, l := range strings.Split(new, "\n") {
		newMap[l] = true
	}
	
	for _, l := range oldLines {
		if !newMap[l] {
			return true // A line that existed in old does not exist in new
		}
	}
	return false
}

func cascadeBumps() {
	// Read all chips to find dependencies
	chipDirs, err := os.ReadDir("chips")
	if err != nil {
		return
	}

	for _, d := range chipDirs {
		if !d.IsDir() {
			continue
		}
		chipName := d.Name()
		chipPath := "chips/" + chipName
		manifestPath := filepath.Join(chipPath, "hardware.json")
		
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		
		var manifest ChipManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		// Check vendor bump
		if bump, ok := componentBumps["vendor/"+manifest.Vendor]; ok {
			setBump(chipPath, bump)
		}
		// Check arch bump
		if bump, ok := componentBumps["arch/"+manifest.Arch]; ok {
			setBump(chipPath, bump)
		}
	}
}

func applyBump(compPath string, bumpType int) {
	if bumpType == BUMP_NONE {
		return
	}

	manifestFile := "vendor_manifest.json"
	if strings.HasPrefix(compPath, "arch/") {
		manifestFile = "arch_manifest.json"
	} else if strings.HasPrefix(compPath, "chips/") {
		manifestFile = "hardware.json"
	}

	manifestPath := filepath.Join(compPath, manifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return
	}

	versionStr, ok := manifest["version"].(string)
	if !ok {
		return
	}

	parts := strings.Split(versionStr, ".")
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

	newVersion := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	manifest["version"] = newVersion
	fmt.Printf("[semver_calc] Bumping %s by %s to version %s\n", compPath, bumpStr, newVersion)

	newData, _ := json.MarshalIndent(manifest, "", "  ")
	newData = append(newData, '\n')
	newData = bytes.ReplaceAll(newData, []byte("\\u003e"), []byte(">"))

	os.WriteFile(manifestPath, newData, 0644)
}
