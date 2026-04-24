package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/adapters/github"
	"github.com/meigma/ghd/internal/config"
)

func TestNewComponentsRefusesTokenForCustomGitHubAPIURL(t *testing.T) {
	_, err := newComponents(context.Background(), config.Config{
		GitHubBaseURL: "https://example.test/api",
		GitHubToken:   "token-123",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to send GitHub token")
	assert.Contains(t, err.Error(), "https://example.test/api")
}

func TestNewComponentsAllowsTokenForDefaultGitHubAPIURL(t *testing.T) {
	_, err := newComponents(context.Background(), config.Config{
		GitHubBaseURL: github.DefaultBaseURL + "/",
		GitHubToken:   "token-123",
	})

	require.NoError(t, err)
}
