package sigstore

import (
	_ "embed"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
)

const githubTUFMirror = "https://tuf-repo.github.com"

// githubTUFRoot is the GitHub Sigstore TUF root from the GitHub CLI reference implementation.
//
// Source: https://github.com/cli/cli/blob/trunk/pkg/cmd/attestation/verification/embed/tuf-repo.github.com/root.json
//
//go:embed github_tuf_root.json
var githubTUFRoot []byte

// FetchGitHubTrustedRoot fetches GitHub's Sigstore trusted root through GitHub's TUF mirror.
func FetchGitHubTrustedRoot() (*root.TrustedRoot, error) {
	opts := tuf.DefaultOptions()
	opts.Root = githubTUFRoot
	opts.RepositoryBaseURL = githubTUFMirror

	client, err := tuf.New(opts)
	if err != nil {
		return nil, fmt.Errorf("create GitHub TUF client: %w", err)
	}
	jsonBytes, err := client.GetTarget("trusted_root.json")
	if err != nil {
		return nil, fmt.Errorf("fetch GitHub trusted root: %w", err)
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub trusted root: %w", err)
	}
	return trustedRoot, nil
}
