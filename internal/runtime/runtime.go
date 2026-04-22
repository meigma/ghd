package runtime

import (
	"context"
	"fmt"

	sigroot "github.com/sigstore/sigstore-go/pkg/root"

	"github.com/meigma/ghd/internal/adapters/filesystem"
	"github.com/meigma/ghd/internal/adapters/github"
	"github.com/meigma/ghd/internal/adapters/sigstore"
	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/verification"
)

const userAgent = "ghd"

// NewVerifiedDownloader wires the verified download use case.
func NewVerifiedDownloader(ctx context.Context, cfg config.Config) (*app.VerifiedDownloader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	githubOptions := []github.Option{github.WithUserAgent(userAgent)}
	if cfg.GitHubBaseURL != "" {
		githubOptions = append(githubOptions, github.WithBaseURL(cfg.GitHubBaseURL))
	}
	if cfg.GitHubToken != "" {
		githubOptions = append(githubOptions, github.WithToken(cfg.GitHubToken))
	}
	githubClient, err := github.NewClient(githubOptions...)
	if err != nil {
		return nil, err
	}

	sigstoreOptions := []sigstore.Option{}
	if cfg.TrustedRootPath != "" {
		sigstoreOptions = append(sigstoreOptions, sigstore.WithTrustedRootPath(cfg.TrustedRootPath))
	} else {
		trustedRoot, err := sigroot.FetchTrustedRoot()
		if err != nil {
			return nil, fmt.Errorf("fetch Sigstore trusted root: %w", err)
		}
		githubTrustedRoot, err := sigstore.FetchGitHubTrustedRoot()
		if err != nil {
			return nil, err
		}
		sigstoreOptions = append(sigstoreOptions,
			sigstore.WithPublicGoodTrustedMaterial(trustedRoot),
			sigstore.WithGitHubTrustedMaterial(githubTrustedRoot),
		)
	}
	bundleVerifier, err := sigstore.NewVerifier(sigstoreOptions...)
	if err != nil {
		return nil, err
	}

	coreVerifier, err := verification.NewVerifier(verification.Dependencies{
		ReleaseResolver:   githubClient,
		AttestationSource: githubClient,
		BundleVerifier:    bundleVerifier,
	})
	if err != nil {
		return nil, err
	}

	return app.NewVerifiedDownloader(app.VerifiedDownloadDependencies{
		Manifests:      githubClient,
		Assets:         githubClient,
		Downloader:     githubClient,
		Verifier:       coreVerifier,
		EvidenceWriter: filesystem.NewEvidenceWriter(),
	})
}
