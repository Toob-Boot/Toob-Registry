package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ComponentVersion struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

type InternalTopology struct {
	Chips      []ComponentVersion `json:"chips"`
	Archs      []ComponentVersion `json:"archs"`
	Vendors    []ComponentVersion `json:"vendors"`
	Toolchains []ComponentVersion `json:"toolchains"`
}

type RegistryRelease struct {
	Version  string           `json:"version"`
	Source   string           `json:"source"`
	Internal InternalTopology `json:"internal_snapshot"`
}

type ExternalTopology struct {
	Registry []RegistryRelease  `json:"registry"`
	Cli      []ComponentVersion `json:"cli"`
	CoreSDK  []ComponentVersion `json:"core_sdk"`
	Compiler []ComponentVersion `json:"compiler"`
}

type VersionTopology struct {
	GeneratedAt string           `json:"generated_at"`
	MainBranch  InternalTopology `json:"main_branch"`
	Official    ExternalTopology `json:"official"`
}

func fetchGitHubPages(url string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Toob-Registry-Topology")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			return nil, fmt.Errorf("failed to fetch")
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

func getCliVersions() []ComponentVersion {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-CLI-Release/releases?per_page=100")
	var versions []ComponentVersion
	if err != nil {
		return versions
	}
	for _, item := range data {
		if tag, ok := item["tag_name"].(string); ok {
			if strings.HasPrefix(tag, "cli/") {
				htmlURL, _ := item["html_url"].(string)
				if htmlURL == "" {
					htmlURL = "https://github.com/Toob-Boot/Toob-CLI-Release/releases/tag/" + tag
				}
				versions = append(versions, ComponentVersion{
					Version: tag,
					Source:  htmlURL,
				})
			}
		}
	}
	return versions
}

func getCoreVersions() []ComponentVersion {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-Loader/tags?per_page=100")
	var versions []ComponentVersion
	if err != nil {
		return versions
	}
	for _, item := range data {
		if name, ok := item["name"].(string); ok {
			if strings.HasPrefix(name, "core/") || strings.HasPrefix(name, "v") {
				versions = append(versions, ComponentVersion{
					Version: name,
					Source:  "https://github.com/Toob-Boot/Toob-Loader/releases/tag/" + name,
				})
			}
		}
	}
	return versions
}

func getRegistryVersions() []RegistryRelease {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-Registry/tags?per_page=100")
	var versions []RegistryRelease
	if err != nil {
		return versions
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, item := range data {
		if name, ok := item["name"].(string); ok {
			release := RegistryRelease{
				Version: name,
				Source:  "https://github.com/Toob-Boot/Toob-Registry/releases/tag/" + name,
			}

			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/Toob-Boot/Toob-Registry/%s/registry.json", name)
			req, _ := http.NewRequest("GET", rawURL, nil)
			if token := os.Getenv("GITHUB_TOKEN"); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == 200 {
				body, _ := io.ReadAll(resp.Body)
				var registry struct {
					Chips      map[string]struct{ Version string } `json:"chips"`
					Archs      map[string]struct{ Version string } `json:"archs"`
					Vendors    map[string]struct{ Version string } `json:"vendors"`
					Toolchains map[string]struct{ Version string } `json:"toolchains"`
				}
				if err := json.Unmarshal(body, &registry); err == nil {
					for cName, cItem := range registry.Chips {
						release.Internal.Chips = append(release.Internal.Chips, ComponentVersion{
							Name: cName, Version: cItem.Version,
							Source: fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/%s/chips/%s", name, cName),
						})
					}
					for cName, cItem := range registry.Archs {
						release.Internal.Archs = append(release.Internal.Archs, ComponentVersion{
							Name: cName, Version: cItem.Version,
							Source: fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/%s/arch/%s", name, cName),
						})
					}
					for cName, cItem := range registry.Vendors {
						release.Internal.Vendors = append(release.Internal.Vendors, ComponentVersion{
							Name: cName, Version: cItem.Version,
							Source: fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/%s/vendor/%s", name, cName),
						})
					}
					for cName, cItem := range registry.Toolchains {
						release.Internal.Toolchains = append(release.Internal.Toolchains, ComponentVersion{
							Name: cName, Version: cItem.Version,
							Source: fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/%s/toolchains/%s", name, cName),
						})
					}
				}
			}
			if resp != nil {
				resp.Body.Close()
			}
			versions = append(versions, release)
		}
	}
	return versions
}

func getCompilerVersions() []ComponentVersion {
	url := "https://hub.docker.com/v2/repositories/mannomannx/toob-compiler/tags/?page_size=100"
	var versions []ComponentVersion

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
			versions = append(versions, ComponentVersion{
				Version: tag.Name,
				Source:  "https://hub.docker.com/r/mannomannx/toob-compiler/tags",
			})
		}

		if result.Next != nil {
			url = *result.Next
		} else {
			url = ""
		}
	}
	return versions
}

func main() {
	fmt.Println("Generating version topology mapping...")

	topology := VersionTopology{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Read internal registry
	regData, err := os.ReadFile("registry.json")
	if err == nil {
		var registry struct {
			Chips      map[string]struct{ Version string } `json:"chips"`
			Archs      map[string]struct{ Version string } `json:"archs"`
			Vendors    map[string]struct{ Version string } `json:"vendors"`
			Toolchains map[string]struct{ Version string } `json:"toolchains"`
		}
		if err := json.Unmarshal(regData, &registry); err == nil {
			for name, item := range registry.Chips {
				topology.MainBranch.Chips = append(topology.MainBranch.Chips, ComponentVersion{
					Name:    name,
					Version: item.Version,
					Source:  fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/main/chips/%s", name),
				})
			}
			for name, item := range registry.Archs {
				topology.MainBranch.Archs = append(topology.MainBranch.Archs, ComponentVersion{
					Name:    name,
					Version: item.Version,
					Source:  fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/main/arch/%s", name),
				})
			}
			for name, item := range registry.Vendors {
				topology.MainBranch.Vendors = append(topology.MainBranch.Vendors, ComponentVersion{
					Name:    name,
					Version: item.Version,
					Source:  fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/main/vendor/%s", name),
				})
			}
			for name, item := range registry.Toolchains {
				topology.MainBranch.Toolchains = append(topology.MainBranch.Toolchains, ComponentVersion{
					Name:    name,
					Version: item.Version,
					Source:  fmt.Sprintf("https://github.com/Toob-Boot/Toob-Registry/tree/main/toolchains/%s", name),
				})
			}
		}
	}

	// Fetch external ecosystem versions
	topology.Official.Registry = getRegistryVersions()
	topology.Official.Cli = getCliVersions()
	topology.Official.CoreSDK = getCoreVersions()
	topology.Official.Compiler = getCompilerVersions()

	out, _ := json.MarshalIndent(topology, "", "    ")
	err = os.WriteFile("version_index.json", out, 0644)
	if err != nil {
		log.Fatalf("FATAL: Error writing version_index.json: %v", err)
	}

	fmt.Println("Successfully wrote version_index.json")
}
