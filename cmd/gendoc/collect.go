package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type repo struct {
	Name   string `json:"name"`
	Branch string `json:"default_branch"`
}

// listRepos names every public repo of the org. the token is optional; it
// only raises the rate limit, which matters on scheduled ci runs.
func listRepos(org string) ([]repo, error) {
	url := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&type=public", org)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}
	var repos []repo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// clone refreshes the cached shallow clone of one repo and returns its path.
// an existing clone is updated in place so repeated runs only pay for the
// delta, never for a full history download.
func clone(r repo, org, clonesDir string) (string, error) {
	dst := filepath.Join(clonesDir, r.Name)
	url := fmt.Sprintf("https://github.com/%s/%s", org, r.Name)

	if _, err := os.Stat(filepath.Join(dst, ".git")); os.IsNotExist(err) {
		args := []string{"clone", "--depth", "1", "--branch", r.Branch, "--single-branch", url, dst}
		cmd := exec.Command("git", args...)
		if _, err := cmd.CombinedOutput(); err != nil {
			// a renamed default branch must not take the whole build down
			cmd = exec.Command("git", "clone", "--depth", "1", "--single-branch", url, dst)
			if out2, err2 := cmd.CombinedOutput(); err2 != nil {
				os.RemoveAll(dst)
				return "", fmt.Errorf("clone %s: %v: %s", r.Name, err2, out2)
			}
		}
		return dst, nil
	}

	steps := [][]string{
		{"fetch", "--depth", "1", "origin", r.Branch},
		{"reset", "--hard", "FETCH_HEAD"},
		{"clean", "-fdq"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %v in %s: %v: %s", args, r.Name, err, out)
		}
	}
	return dst, nil
}
