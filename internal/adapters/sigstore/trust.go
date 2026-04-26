package sigstore

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

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

// TrustedRootChecker validates custom trusted_root.json documents.
type TrustedRootChecker struct{}

// NewTrustedRootChecker creates a trusted root checker.
func NewTrustedRootChecker() TrustedRootChecker {
	return TrustedRootChecker{}
}

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

// ValidateTrustedRoot checks that path exists and can build a usable Sigstore verifier.
func (TrustedRootChecker) ValidateTrustedRoot(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("trusted root path must be set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read trusted root: %w", err)
	}
	if _, err := NewVerifier(WithTrustedRootJSON(data)); err != nil {
		return err
	}
	return nil
}
