package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config contains runtime settings used to wire adapters.
type Config struct {
	// GitHubBaseURL optionally overrides the GitHub REST API base URL.
	GitHubBaseURL string
	// GitHubToken is an optional bearer token for GitHub API requests.
	GitHubToken string
	// TrustedRootPath optionally points at a Sigstore trusted_root.json file.
	TrustedRootPath string
	// IndexDir is the local repository catalog directory.
	IndexDir string
	// StoreDir is the root of ghd's managed package store.
	StoreDir string
	// BinDir receives links to installed binaries.
	BinDir string
}

// Load reads runtime settings from Viper and the process environment.
func Load(vp *viper.Viper) Config {
	token := strings.TrimSpace(vp.GetString("github-token"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GH_TOKEN"))
	}
	storeDir := strings.TrimSpace(vp.GetString("store-dir"))
	binDir := strings.TrimSpace(vp.GetString("bin-dir"))
	indexDir := strings.TrimSpace(vp.GetString("index-dir"))
	home, _ := os.UserHomeDir()
	if indexDir == "" && home != "" {
		indexDir = filepath.Join(home, ".local", "share", "ghd", "index")
	}
	if storeDir == "" && home != "" {
		storeDir = filepath.Join(home, ".local", "share", "ghd", "store")
	}
	if binDir == "" && home != "" {
		binDir = filepath.Join(home, ".local", "bin")
	}
	return Config{
		GitHubBaseURL:   strings.TrimSpace(vp.GetString("github-api-url")),
		GitHubToken:     token,
		TrustedRootPath: strings.TrimSpace(vp.GetString("trusted-root")),
		IndexDir:        indexDir,
		StoreDir:        storeDir,
		BinDir:          binDir,
	}
}
