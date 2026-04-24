package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

// ManifestSource fetches repository ghd.toml bytes.
type ManifestSource interface {
	// FetchManifest returns the raw root ghd.toml for repository.
	FetchManifest(ctx context.Context, repository verification.Repository) ([]byte, error)
}

// ReleaseAssetSource resolves release assets.
type ReleaseAssetSource interface {
	// ResolveReleaseAsset resolves a concrete release asset name for tag.
	ResolveReleaseAsset(ctx context.Context, repository verification.Repository, tag verification.ReleaseTag, assetName string) (ReleaseAsset, error)
}

// ArtifactDownloader downloads release assets.
type ArtifactDownloader interface {
	// DownloadReleaseAsset downloads asset into outputDir and returns the local artifact path.
	DownloadReleaseAsset(ctx context.Context, request DownloadReleaseAssetRequest) (string, error)
}

// EvidenceWriter records verification evidence.
type EvidenceWriter interface {
	// WriteVerificationEvidence writes record into outputDir and returns the evidence path.
	WriteVerificationEvidence(ctx context.Context, outputDir string, record VerificationRecord) (string, error)
}

// Verifier verifies downloaded release assets.
type Verifier interface {
	// VerifyReleaseAsset verifies one downloaded release asset.
	VerifyReleaseAsset(ctx context.Context, request verification.Request) (verification.Evidence, error)
}

// ReleaseAsset contains adapter-neutral release asset download metadata.
type ReleaseAsset struct {
	// Name is the concrete GitHub release asset name.
	Name string
	// DownloadURL is the URL used by an adapter to download the asset.
	DownloadURL string
}

// DownloadProgress describes byte-level artifact download progress.
type DownloadProgress struct {
	// AssetName is the concrete GitHub release asset being downloaded.
	AssetName string
	// BytesDownloaded is the number of bytes durably written so far.
	BytesDownloaded int64
	// TotalBytes is the expected asset size. Zero means unknown.
	TotalBytes int64
}

// DownloadProgressFunc receives byte-level artifact download progress.
type DownloadProgressFunc func(DownloadProgress)

// DownloadReleaseAssetRequest describes one artifact download.
type DownloadReleaseAssetRequest struct {
	// Asset is the concrete GitHub release asset to download.
	Asset ReleaseAsset
	// OutputDir receives the downloaded artifact.
	OutputDir string
	// Progress receives byte-level download progress. Nil disables progress reports.
	Progress DownloadProgressFunc
}

// VerifiedDownloadDependencies contains the ports needed by VerifiedDownloader.
type VerifiedDownloadDependencies struct {
	// Manifests fetches repository manifest bytes.
	Manifests ManifestSource
	// Assets resolves concrete release assets.
	Assets ReleaseAssetSource
	// Downloader downloads concrete release assets.
	Downloader ArtifactDownloader
	// Verifier verifies downloaded assets.
	Verifier Verifier
	// EvidenceWriter records verification evidence.
	EvidenceWriter EvidenceWriter
}

// VerifiedDownloader implements the verified download use case.
type VerifiedDownloader struct {
	manifests ManifestSource
	assets    ReleaseAssetSource
	download  ArtifactDownloader
	verify    Verifier
	evidence  EvidenceWriter
}

// VerifiedDownloadRequest describes one verified download.
type VerifiedDownloadRequest struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName string
	// Version is the literal version value used for manifest pattern expansion.
	Version string
	// OutputDir receives the downloaded artifact and verification evidence.
	OutputDir string
	// Platform optionally overrides the current OS/architecture.
	Platform manifest.Platform
}

// VerifiedDownloadResult describes a completed verified download.
type VerifiedDownloadResult struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName string
	// Version is the literal requested version.
	Version string
	// Tag is the resolved GitHub release tag.
	Tag verification.ReleaseTag
	// AssetName is the concrete release asset name.
	AssetName string
	// ArtifactPath is the local downloaded artifact path.
	ArtifactPath string
	// EvidencePath is the local verification evidence path.
	EvidencePath string
	// Evidence is the accepted release and provenance verification evidence.
	Evidence verification.Evidence
}

// VerificationRecord is the JSON record written after verification succeeds.
type VerificationRecord struct {
	// SchemaVersion is the verification record schema version.
	SchemaVersion int `json:"schema_version"`
	// Repository is the verified GitHub repository.
	Repository string `json:"repository"`
	// Package is the verified package name.
	Package string `json:"package"`
	// Version is the requested package version.
	Version string `json:"version"`
	// Tag is the resolved release tag.
	Tag string `json:"tag"`
	// Asset is the verified release asset name.
	Asset string `json:"asset"`
	// Evidence is the accepted verification evidence from the core verifier.
	Evidence verification.Evidence `json:"evidence"`
}

// Validate checks one persisted verification record.
func (r VerificationRecord) Validate() error {
	if r.SchemaVersion != 1 {
		return fmt.Errorf("unsupported verification record version %d", r.SchemaVersion)
	}
	if _, err := parseRecordRepository(r.Repository); err != nil {
		return fmt.Errorf("verification repository must be owner/repo")
	}
	fields := []struct {
		label string
		value string
	}{
		{label: "package", value: r.Package},
		{label: "version", value: r.Version},
		{label: "tag", value: r.Tag},
		{label: "asset", value: r.Asset},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("verification %s must be set", field.label)
		}
	}
	if r.Evidence.Repository.IsZero() {
		return fmt.Errorf("verification evidence repository must be set")
	}
	if !strings.EqualFold(r.Evidence.Repository.String(), r.Repository) {
		return fmt.Errorf("verification evidence repository %s does not match record repository %s", r.Evidence.Repository, r.Repository)
	}
	if strings.TrimSpace(string(r.Evidence.Tag)) == "" {
		return fmt.Errorf("verification evidence tag must be set")
	}
	if string(r.Evidence.Tag) != r.Tag {
		return fmt.Errorf("verification evidence tag %s does not match record tag %s", r.Evidence.Tag, r.Tag)
	}
	if r.Evidence.AssetDigest.IsZero() {
		return fmt.Errorf("verification evidence asset digest must be set")
	}
	return nil
}

// NewVerifiedDownloader creates a verified download use case.
func NewVerifiedDownloader(deps VerifiedDownloadDependencies) (*VerifiedDownloader, error) {
	if deps.Manifests == nil {
		return nil, fmt.Errorf("manifest source must be set")
	}
	if deps.Assets == nil {
		return nil, fmt.Errorf("release asset source must be set")
	}
	if deps.Downloader == nil {
		return nil, fmt.Errorf("artifact downloader must be set")
	}
	if deps.Verifier == nil {
		return nil, fmt.Errorf("verifier must be set")
	}
	if deps.EvidenceWriter == nil {
		return nil, fmt.Errorf("evidence writer must be set")
	}
	return &VerifiedDownloader{
		manifests: deps.Manifests,
		assets:    deps.Assets,
		download:  deps.Downloader,
		verify:    deps.Verifier,
		evidence:  deps.EvidenceWriter,
	}, nil
}

// Download fetches, verifies, and records one release asset.
func (d *VerifiedDownloader) Download(ctx context.Context, request VerifiedDownloadRequest) (VerifiedDownloadResult, error) {
	if err := request.validate(); err != nil {
		return VerifiedDownloadResult{}, err
	}
	platform := request.Platform.WithDefaults()

	manifestBytes, err := d.manifests.FetchManifest(ctx, request.Repository)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	cfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	pkg, err := cfg.Package(request.PackageName)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	tag, err := pkg.ReleaseTag(request.Version)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	selected, err := pkg.SelectAsset(platform, request.Version)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	releaseAsset, err := d.assets.ResolveReleaseAsset(ctx, request.Repository, tag, selected.Name)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("resolve release asset %q: %w", selected.Name, err)
	}
	artifactPath, err := d.download.DownloadReleaseAsset(ctx, DownloadReleaseAssetRequest{
		Asset:     releaseAsset,
		OutputDir: request.OutputDir,
	})
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("download release asset %q: %w", releaseAsset.Name, err)
	}
	evidence, err := d.verify.VerifyReleaseAsset(ctx, verification.Request{
		Repository: request.Repository,
		Tag:        tag,
		AssetPath:  artifactPath,
		Policy: verification.Policy{
			TrustedSignerWorkflow: cfg.Provenance.TrustedSignerWorkflow(),
		},
	})
	if err != nil {
		return VerifiedDownloadResult{}, err
	}

	record := VerificationRecord{
		SchemaVersion: 1,
		Repository:    request.Repository.String(),
		Package:       request.PackageName,
		Version:       request.Version,
		Tag:           string(tag),
		Asset:         selected.Name,
		Evidence:      evidence,
	}
	evidencePath, err := d.evidence.WriteVerificationEvidence(ctx, request.OutputDir, record)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("write verification evidence: %w", err)
	}

	return VerifiedDownloadResult{
		Repository:   request.Repository,
		PackageName:  request.PackageName,
		Version:      request.Version,
		Tag:          tag,
		AssetName:    selected.Name,
		ArtifactPath: artifactPath,
		EvidencePath: evidencePath,
		Evidence:     evidence,
	}, nil
}

func (r VerifiedDownloadRequest) validate() error {
	if strings.TrimSpace(r.Repository.Owner) == "" || strings.TrimSpace(r.Repository.Name) == "" {
		return fmt.Errorf("repository must be owner/repo")
	}
	if strings.Contains(r.Repository.Owner, "/") || strings.Contains(r.Repository.Name, "/") {
		return fmt.Errorf("repository must be owner/repo")
	}
	if strings.TrimSpace(r.PackageName) == "" {
		return fmt.Errorf("package name must be set")
	}
	if strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("version must be set")
	}
	if strings.TrimSpace(r.OutputDir) == "" {
		return fmt.Errorf("output directory must be set")
	}
	return nil
}
