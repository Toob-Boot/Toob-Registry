package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type ReleasesIndex struct {
	Cli      []string `json:"cli"`
	CoreSDK  []string `json:"core_sdk"`
	Compiler []string `json:"compiler"`
}

// fetchGitHubPages handles Link header pagination for GitHub API.
func fetchGitHubPages(url string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Toob-Registry-Release-Sync")
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

func getActiveCliVersions() []string {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-CLI-Release/releases?per_page=100")
	if err != nil {
		log.Printf("Warning: Failed to fetch CLI releases: %v", err)
		return []string{"main"}
	}
	var versions []string
	for _, item := range data {
		if tag, ok := item["tag_name"].(string); ok {
			versions = append(versions, tag)
		}
	}
	if len(versions) == 0 {
		versions = append(versions, "main")
	}
	return versions
}

func getActiveCoreVersions() []string {
	data, err := fetchGitHubPages("https://api.github.com/repos/Toob-Boot/Toob-Loader/tags?per_page=100")
	if err != nil {
		log.Printf("Warning: Failed to fetch Core SDK tags: %v", err)
		return []string{"main"}
	}
	var versions []string
	for _, item := range data {
		if name, ok := item["name"].(string); ok {
			if strings.HasPrefix(name, "core/") {
				versions = append(versions, strings.TrimPrefix(name, "core/"))
			}
		}
	}
	if len(versions) == 0 {
		versions = append(versions, "main")
	}
	return versions
}

func getActiveCompilerVersions() []string {
	url := "https://hub.docker.com/v2/repositories/repowatt/toob-compiler/tags/?page_size=100"
	var versions []string
	
	for url != "" {
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != 200 {
			log.Printf("Warning: Failed to fetch Compiler tags: %v", err)
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
			versions = append(versions, tag.Name)
		}
		
		if result.Next != nil {
			url = *result.Next
		} else {
			url = ""
		}
	}
	
	if len(versions) == 0 {
		return []string{"latest"}
	}
	return versions
}

func main() {
	fmt.Println("Syncing external ecosystem releases (CLI, Core, Compiler)...")
	
	index := ReleasesIndex{
		Cli:      getActiveCliVersions(),
		CoreSDK:  getActiveCoreVersions(),
		Compiler: getActiveCompilerVersions(),
	}
	
	out, _ := json.MarshalIndent(index, "", "    ")
	os.WriteFile("releases.json", out, 0644)
	fmt.Println("Successfully wrote releases.json")
}
