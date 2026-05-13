package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

type ChipManifest struct {
	Name           string `json:"name"`
	Vendor         string `json:"vendor"`
	Arch           string `json:"arch"`
	CompilerPrefix string `json:"compiler_prefix"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	Path           string `json:"path,omitempty"`
	MinCoreSDK     string `json:"min_core_sdk,omitempty"`
	MinCompiler    string `json:"min_compiler,omitempty"`
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

type ReleasesIndex struct {
	Cli      []string `json:"cli"`
	CoreSDK  []string `json:"core_sdk"`
	Compiler []string `json:"compiler"`
}

type Registry struct {
	Releases   *ReleasesIndex            `json:"releases,omitempty"`
	Chips      map[string]ChipManifest   `json:"chips"`
	Toolchains map[string]ToolchainEntry `json:"toolchains"`
	Vendors    map[string]VendorEntry    `json:"vendors"`
	Archs      map[string]ArchEntry      `json:"archs"`
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
	EnvironmentHash     string                           `json:"environment_hash"`
	Dependencies        Dependencies                     `json:"dependencies"`
	VerifiedCombinations map[string]VerifiedCombination `json:"verified_combinations"`
}

type MatrixChip struct {
	Versions map[string]MatrixVersion `json:"versions"`
}

type Matrix map[string]MatrixChip

type InternalStateEntry struct {
	Status     string `json:"status"`
	LastTested string `json:"last_tested"`
	RetryCount int    `json:"retry_count"`
}

type InternalState struct {
	Combinations map[string]InternalStateEntry `json:"combinations"`
}

type Target struct {
	Chip     string `json:"chip"`
	Cli      string `json:"cli"`
	Core     string `json:"core"`
	Compiler string `json:"compiler"`
}

// fetchGitHubPages handles Link header pagination for GitHub API.
func fetchGitHubPages(url string) ([]map[string]interface{}, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var results []map[string]interface{}
	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Toob-Registry-Matrix-Generator")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP request failed: %w", err)
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := resp.Header.Get("Retry-After")
			resp.Body.Close()
			if retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
					log.Printf("[MatrixGen] Rate limited, waiting %ds", secs)
					time.Sleep(time.Duration(secs) * time.Second)
					continue
				}
			}
			return nil, fmt.Errorf("rate limited (HTTP %d)", resp.StatusCode)
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
		}

		body, _ := io.ReadAll(resp.Body)
		var page []map[string]interface{}
		if err := json.Unmarshal(body, &page); err != nil {
			resp.Body.Close()
			return nil, err
		}
		results = append(results, page...)

		url = ""
		linkHeader := resp.Header.Get("Link")
		if linkHeader != "" {
			parts := strings.Split(linkHeader, ",")
			for _, part := range parts {
				if strings.Contains(part, `rel="next"`) {
					start := strings.Index(part, "<")
					end := strings.Index(part, ">")
					if start != -1 && end != -1 {
						url = part[start+1 : end]
					}
					break
				}
			}
		}
		resp.Body.Close()
	}
	return results, nil
}

// getActiveCliVersions fetches all tagged releases from the CLI release repo.
// Only immutable, tagged versions are returned — "main" is excluded because
// it is a moving target whose test results become stale on the next commit.
func getActiveCliVersions() []string {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-CLI-Release/releases?per_page=100")
	if err != nil {
		log.Printf("[MatrixGen] Warning: Could not fetch CLI releases: %v", err)
		return nil
	}
	var versions []string
	for _, item := range data {
		if tag, ok := item["tag_name"].(string); ok {
			versions = append(versions, tag)
		}
	}
	return versions
}

// normalizeVersion removes common prefixes like 'core/' or 'v' for clean comparisons.
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "core/")
	v = strings.TrimPrefix(v, "v")
	return v
}

// getActiveCoreVersions fetches all core/* releases from the Toob-Loader repo.
// Only officially published releases are returned.
func getActiveCoreVersions() []string {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-Loader/releases?per_page=100")
	if err != nil {
		log.Printf("[MatrixGen] Warning: Could not fetch Core releases: %v", err)
		return nil
	}
	var versions []string
	for _, item := range data {
		if tag, ok := item["tag_name"].(string); ok {
			if strings.HasPrefix(tag, "core/") {
				versions = append(versions, normalizeVersion(tag))
			}
		}
	}
	return versions
}

// getActiveCompilerVersions returns the current compiler tags to test.
func getActiveCompilerVersions() []string {
	url := "https://hub.docker.com/v2/repositories/mannomannx/toob-compiler/tags/?page_size=100"
	var versions []string
	
	for url != "" {
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != 200 {
			break
		}
		
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		var result struct {
			Next    *string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		
		if err := json.Unmarshal(body, &result); err != nil {
			break
		}
		
		for _, tag := range result.Results {
			if tag.Name == "latest" {
				continue
			}
			versions = append(versions, tag.Name)
		}
		
		if result.Next != nil {
			url = *result.Next
		} else {
			url = ""
		}
	}
	
	if len(versions) == 0 {
		log.Println("[MatrixGen] Warning: No pinned compiler versions found on DockerHub.")
		return nil
	}
	return versions
}

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

	// Read Internal State (Git-tracked DB for pending/failed states)
	stateData, err := os.ReadFile("internal_state.json")
	var internalState InternalState
	if err == nil {
		json.Unmarshal(stateData, &internalState)
	}
	if internalState.Combinations == nil {
		internalState.Combinations = make(map[string]InternalStateEntry)
	}

	targetChip := os.Getenv("CHIP")
	var cliVersions []string
	var coreVersions []string
	var compilerVersions []string

	if registry.Releases != nil {
		cliVersions = registry.Releases.Cli
		coreVersions = registry.Releases.CoreSDK
		compilerVersions = registry.Releases.Compiler
	}

	if len(cliVersions) == 0 {
		cliVersions = getActiveCliVersions()
	}
	if len(coreVersions) == 0 {
		coreVersions = getActiveCoreVersions()
	}
	if len(compilerVersions) == 0 {
		compilerVersions = getActiveCompilerVersions()
	}
	
	testQueue := []Target{}

	for chipKey, chip := range registry.Chips {
		tcName := chip.CompilerPrefix
		if len(tcName) > 0 && tcName[len(tcName)-1] == '-' {
			tcName = tcName[:len(tcName)-1]
		}
		
		toolchain, exists := registry.Toolchains[tcName]
		if !exists {
			continue
		}

		// Calculate pristine Hardware Hash (WITHOUT CLI version)
		vManifest := registry.Vendors[chip.Vendor]
		aManifest := registry.Archs[chip.Arch]
		
		chipBytes, _ := json.Marshal(chip)
		tcBytes, _ := json.Marshal(toolchain)
		vBytes, _ := json.Marshal(vManifest)
		aBytes, _ := json.Marshal(aManifest)
		
		h := sha256.New()
		h.Write(chipBytes)
		h.Write(tcBytes)
		h.Write(vBytes)
		h.Write(aBytes)
		hardwareHash := hex.EncodeToString(h.Sum(nil))

		// If called with specific CHIP, just output its hash and exit
		if targetChip == chipKey {
			fmt.Print(hardwareHash)
			os.Exit(0)
		}

		// Calculate T - A (Target minus Actual)
		matrixEntry, chipExists := matrix[chipKey]
		var verifiedMap map[string]VerifiedCombination
		
		if chipExists {
			if versionEntry, versionExists := matrixEntry.Versions[chip.Version]; versionExists && versionEntry.EnvironmentHash == hardwareHash {
				if versionEntry.VerifiedCombinations != nil {
					verifiedMap = versionEntry.VerifiedCombinations
				} else {
					verifiedMap = make(map[string]VerifiedCombination)
				}
			} else {
				verifiedMap = make(map[string]VerifiedCombination)
			}
		} else {
			verifiedMap = make(map[string]VerifiedCombination)
		}

		// Compare Cartesian Product
		for _, cli := range cliVersions {
			for _, core := range coreVersions {
				for _, compiler := range compilerVersions {
					// Build tuple key
					tupleKey := fmt.Sprintf("cli=%s::core=%s::compiler=%s", cli, core, compiler)
					
					// SemVer Filtering for Core SDK
					if chip.MinCoreSDK != "" && chip.MinCoreSDK != "main" && core != "main" {
						vCore := "v" + normalizeVersion(core)
						vMin := "v" + normalizeVersion(chip.MinCoreSDK)
						if semver.IsValid(vCore) && semver.IsValid(vMin) {
							if semver.Compare(vCore, vMin) < 0 {
								continue
							}
						}
					}

					// SemVer Filtering for Compiler
					if chip.MinCompiler != "" && chip.MinCompiler != "latest" && compiler != "latest" {
						vComp := "v" + normalizeVersion(compiler)
						vMinC := "v" + normalizeVersion(chip.MinCompiler)
						if semver.IsValid(vComp) && semver.IsValid(vMinC) {
							if semver.Compare(vComp, vMinC) < 0 {
								continue
							}
						}
					}

					if verifiedMap[tupleKey].Status == "VERIFIED" {
						continue
					}
					
					// Check Internal State for errors and retries
					if entry, exists := internalState.Combinations[tupleKey]; exists {
						if entry.Status == "FATAL_INFRA_ERROR" {
							// Gap #23: Auto-reset after 30 days
							if t, err := time.Parse(time.RFC3339, entry.LastTested); err == nil {
								if time.Since(t) < 30*24*time.Hour {
									continue
								}
							} else {
								continue
							}
						}

						if t, err := time.Parse(time.RFC3339, entry.LastTested); err == nil {
							if entry.Status == "COMPILER_ERROR" {
								if time.Since(t) < 30*24*time.Hour {
									continue
								}
							} else if entry.Status == "INFRA_ERROR" {
								if time.Since(t) < 2*time.Hour {
									continue
								}
							}
						}
					}

					testQueue = append(testQueue, Target{
						Chip:     chipKey,
						Cli:      cli,
						Core:     core,
						Compiler: compiler,
					})

					if len(testQueue) >= 256 {
						break
					}
				}
				if len(testQueue) >= 256 {
					break
				}
			}
			if len(testQueue) >= 256 {
				break
			}
		}
		if len(testQueue) >= 256 {
			break
		}
	}

	if targetChip != "" {
		fmt.Print("dummyhash")
		os.Exit(1)
	}

	// Job limit logic to prevent CI explosions
	if len(testQueue) > 256 {
		testQueue = testQueue[:256]
	}

	out, _ := json.Marshal(testQueue)
	fmt.Println(string(out))
}
