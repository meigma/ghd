package runtime

import (
	"context"
	"fmt"

	sigroot "github.com/sigstore/sigstore-go/pkg/root"

	"github.com/meigma/ghd/internal/adapters/archive"
	"github.com/meigma/ghd/internal/adapters/filesystem"
	"github.com/meigma/ghd/internal/adapters/github"
	"github.com/meigma/ghd/internal/adapters/sigstore"
	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/verification"
)

const userAgent = "ghd"

// Runtime contains the application use cases wired to concrete adapters.
type Runtime struct {
	downloader *app.VerifiedDownloader
	installer  *app.VerifiedInstaller
}

// New wires the application runtime.
func New(ctx context.Context, cfg config.Config) (*Runtime, error) {
	components, err := newComponents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	downloader, err := app.NewVerifiedDownloader(app.VerifiedDownloadDependencies{
		Manifests:      components.githubClient,
		Assets:         components.githubClient,
		Downloader:     components.githubClient,
		Verifier:       components.coreVerifier,
		EvidenceWriter: components.evidenceWriter,
	})
	if err != nil {
		return nil, err
	}
	installer, err := app.NewVerifiedInstaller(app.VerifiedInstallDependencies{
		Manifests:      components.githubClient,
		Assets:         components.githubClient,
		Downloader:     components.githubClient,
		Verifier:       components.coreVerifier,
		EvidenceWriter: components.evidenceWriter,
		Archives:       archive.NewTarGzipExtractor(),
		FileSystem:     filesystem.NewInstaller(),
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		downloader: downloader,
		installer:  installer,
	}, nil
}

// NewVerifiedDownloader wires the verified download use case.
func NewVerifiedDownloader(ctx context.Context, cfg config.Config) (*app.VerifiedDownloader, error) {
	components, err := newComponents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return app.NewVerifiedDownloader(app.VerifiedDownloadDependencies{
		Manifests:      components.githubClient,
		Assets:         components.githubClient,
		Downloader:     components.githubClient,
		Verifier:       components.coreVerifier,
		EvidenceWriter: components.evidenceWriter,
	})
}

// Download fetches, verifies, and records one release asset.
func (r *Runtime) Download(ctx context.Context, request app.VerifiedDownloadRequest) (app.VerifiedDownloadResult, error) {
	return r.downloader.Download(ctx, request)
}

// Install fetches, verifies, extracts, links, and records one package install.
func (r *Runtime) Install(ctx context.Context, request app.VerifiedInstallRequest) (app.VerifiedInstallResult, error) {
	return r.installer.Install(ctx, request)
}

type components struct {
	githubClient   *github.Client
	coreVerifier   *verification.Verifier
	evidenceWriter filesystem.EvidenceWriter
}

func newComponents(ctx context.Context, cfg config.Config) (components, error) {
	if err := ctx.Err(); err != nil {
		return components{}, err
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
		return components{}, err
	}

	sigstoreOptions := []sigstore.Option{}
	if cfg.TrustedRootPath != "" {
		sigstoreOptions = append(sigstoreOptions, sigstore.WithTrustedRootPath(cfg.TrustedRootPath))
	} else {
		trustedRoot, err := sigroot.FetchTrustedRoot()
		if err != nil {
			return components{}, fmt.Errorf("fetch Sigstore trusted root: %w", err)
		}
		githubTrustedRoot, err := sigstore.FetchGitHubTrustedRoot()
		if err != nil {
			return components{}, err
		}
		sigstoreOptions = append(sigstoreOptions,
			sigstore.WithPublicGoodTrustedMaterial(trustedRoot),
			sigstore.WithGitHubTrustedMaterial(githubTrustedRoot),
		)
	}
	bundleVerifier, err := sigstore.NewVerifier(sigstoreOptions...)
	if err != nil {
		return components{}, err
	}

	coreVerifier, err := verification.NewVerifier(verification.Dependencies{
		ReleaseResolver:   githubClient,
		AttestationSource: githubClient,
		BundleVerifier:    bundleVerifier,
	})
	if err != nil {
		return components{}, err
	}

	return components{
		githubClient:   githubClient,
		coreVerifier:   coreVerifier,
		evidenceWriter: filesystem.NewEvidenceWriter(),
	}, nil
}
