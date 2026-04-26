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
	// StateDir is the local installed package state directory.
	StateDir string
}

// Load reads runtime settings from Viper and the process environment.
func Load(vp *viper.Viper) Config {
	home, _ := os.UserHomeDir()
	return Config{
		GitHubBaseURL:   strings.TrimSpace(vp.GetString("github-api-url")),
		GitHubToken:     githubToken(vp),
		TrustedRootPath: strings.TrimSpace(vp.GetString("trusted-root")),
		IndexDir:        configDir(vp, "index-dir", home, ".local", "share", "ghd", "index"),
		StoreDir:        configDir(vp, "store-dir", home, ".local", "share", "ghd", "store"),
		BinDir:          configDir(vp, "bin-dir", home, ".local", "bin"),
		StateDir:        configDir(vp, "state-dir", home, ".local", "state", "ghd"),
	}
}

func githubToken(vp *viper.Viper) string {
	for _, candidate := range []string{
		vp.GetString("github-token"),
		os.Getenv("GITHUB_TOKEN"),
		os.Getenv("GH_TOKEN"),
	} {
		if token := strings.TrimSpace(candidate); token != "" {
			return token
		}
	}
	return ""
}

func configDir(vp *viper.Viper, key string, home string, defaultPath ...string) string {
	value := strings.TrimSpace(vp.GetString(key))
	if value != "" || home == "" {
		return value
	}
	return filepath.Join(append([]string{home}, defaultPath...)...)
}
