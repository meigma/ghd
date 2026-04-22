package config

import (
	"os"
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
	return Config{
		GitHubBaseURL:   strings.TrimSpace(vp.GetString("github-api-url")),
		GitHubToken:     token,
		TrustedRootPath: strings.TrimSpace(vp.GetString("trusted-root")),
	}
}
