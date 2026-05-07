package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Target struct {
	Chip     string `json:"chip"`
	Cli      string `json:"cli"`
	Core     string `json:"core"`
	Compiler string `json:"compiler"`
}

const (
	ConcurrentWorkers = 3
	BatchSize         = 20
)

func main() {
	fmt.Println("=== Starting Toob Matrix Farm Orchestrator ===")

	for {
		queue := generateMatrixQueue()
		if len(queue) == 0 {
			fmt.Println("No targets to test. Sleeping for 1 hour...")
			time.Sleep(1 * time.Hour)
			continue
		}

		fmt.Printf("Found %d targets in the queue.\n", len(queue))
		processQueue(queue)
		
		fmt.Println("Queue processed. Sleeping for 10 minutes before next sweep...")
		time.Sleep(10 * time.Minute)
	}
}

func generateMatrixQueue() []Target {
	cmd := exec.Command("go", "run", "scripts/matrix_generator.go")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to generate matrix: %v", err)
		return nil
	}

	var queue []Target
	if err := json.Unmarshal(out, &queue); err != nil {
		log.Printf("Failed to parse matrix output: %v", err)
		return nil
	}
	return queue
}

func processQueue(queue []Target) {
	var wg sync.WaitGroup
	targetChan := make(chan Target, len(queue))
	resultChan := make(chan bool, len(queue))

	for _, t := range queue {
		targetChan <- t
	}
	close(targetChan)

	// Spawn workers
	for i := 0; i < ConcurrentWorkers; i++ {
		wg.Add(1)
		go worker(i, targetChan, resultChan)
	}

	// Commit batching goroutine
	go func() {
		count := 0
		for range resultChan {
			count++
			if count%BatchSize == 0 {
				commitLedger()
			}
		}
	}()

	wg.Wait()
	
	// Final commit
	commitLedger()
}

func worker(id int, targets <-chan Target, results chan<- bool) {
	for t := range targets {
		fmt.Printf("[Worker %d] Testing %s (CLI: %s, Core: %s, Compiler: %s)\n", id, t.Chip, t.Cli, t.Core, t.Compiler)
		
		status, err := testTarget(id, t)
		if err != nil {
			fmt.Printf("[Worker %d] Test FAILED (%s): %v\n", id, status, err)
		}

		// Re-calculate hash for this specific target
		hashCmd := exec.Command("go", "run", "scripts/matrix_generator.go")
		hashCmd.Env = append(os.Environ(), "CHIP="+t.Chip)
		hashOut, _ := hashCmd.Output()
		hash = strings.TrimSpace(string(hashOut))

		// Write result
		safeCore := strings.ReplaceAll(t.Core, "/", "-")
		resFile := fmt.Sprintf("result_%s_%s_%s.json", t.Chip, t.Cli, safeCore)
		resData := fmt.Sprintf(`{"chip":"%s", "cli_version":"%s", "core_version":"%s", "compiler_version":"%s", "status":"%s", "state_hash":"%s"}`,
			t.Chip, t.Cli, t.Core, t.Compiler, status, hash)
		
		os.WriteFile(resFile, []byte(resData), 0644)
		
		results <- true
	}
}

func testTarget(workerId int, t Target) (string, error) {
	workDir := filepath.Join(os.TempDir(), fmt.Sprintf("matrix_worker_%d", workerId))
	os.RemoveAll(workDir)
	os.MkdirAll(workDir, 0755)

	cliPath, err := ensureCliVersion(t.Cli)
	if err != nil {
		return "INFRA_ERROR", fmt.Errorf("ensureCliVersion failed: %v", err)
	}

	// 1. Init dummy app
	initCmd := exec.Command(cliPath, "init", "dummy-app", "--chip", t.Chip)
	initCmd.Dir = workDir
	// Point CLI to local registry clone to test the active PR/Code
	cwd, _ := os.Getwd()
	initCmd.Env = append(os.Environ(), "TOOB_REGISTRY_DIR="+cwd)
	
	if out, err := initCmd.CombinedOutput(); err != nil {
		return "INFRA_ERROR", fmt.Errorf("toob init failed: %s", string(out))
	}

	// 2. Modify device.toml
	dummyDir := filepath.Join(workDir, "dummy-app")
	deviceToml := filepath.Join(dummyDir, "device.toml")
	
	f, err := os.OpenFile(deviceToml, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "INFRA_ERROR", err
	}
	f.WriteString(fmt.Sprintf("\n[build]\ncore_sdk = \"%s\"\ncompiler = \"%s\"\n", t.Core, t.Compiler))
	f.Close()

	// 3. Build native
	buildCmd := exec.Command(cliPath, "build", "--native")
	buildCmd.Dir = dummyDir
	buildCmd.Env = append(os.Environ(), "TOOB_REGISTRY_DIR="+cwd, "TOOB_COMPILER_DIR=/tmp/Toob-Loader")
	
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return "COMPILER_ERROR", fmt.Errorf("toob build failed: %s", string(out))
	}

	return "VERIFIED", nil
}

func ensureCliVersion(version string) (string, error) {
	cacheDir := "/cache/cli"
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.MkdirAll(cacheDir, 0755)
	}

	binPath := filepath.Join(cacheDir, fmt.Sprintf("toob-%s", version))
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	// Not in cache, download it
	var url string
	if version == "main" {
		url = "https://github.com/Toob-Boot/Toob-CLI-Release/releases/latest/download/toob-linux-amd64"
	} else {
		url = fmt.Sprintf("https://github.com/Toob-Boot/Toob-CLI-Release/releases/download/%s/toob-linux-amd64", version)
	}

	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to download CLI %s: HTTP %d", version, resp.StatusCode)
	}
	defer resp.Body.Close()

	out, err := os.OpenFile(binPath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return "", err
	}
	defer out.Close()

	io.Copy(out, resp.Body)
	return binPath, nil
}

func commitLedger() {
	// Merge results
	mergeCmd := exec.Command("go", "run", "scripts/update_ledger.go")
	mergeCmd.Run()

	// Cleanup results
	files, _ := filepath.Glob("result_*.json")
	for _, f := range files {
		os.Remove(f)
	}

	// Git Commit & Push
	gitAdd := exec.Command("git", "add", "compatibility_matrix.json", "internal_state.json")
	gitAdd.Run()

	gitCommit := exec.Command("git", "commit", "-m", "chore(ci): matrix farm batch update [skip ci]")
	if err := gitCommit.Run(); err == nil {
		fmt.Println("Ledger batch committed. Rebase-Pulling and Pushing to origin...")
		gitPull := exec.Command("git", "pull", "--rebase")
		gitPull.Run()
		
		gitPush := exec.Command("git", "push")
		gitPush.Run()
	}
}
