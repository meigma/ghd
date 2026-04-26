package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

// ManifestSource fetches repository ghd.toml bytes.
type ManifestSource interface {
	// FetchManifest returns the raw root ghd.toml for repository.
	FetchManifest(ctx context.Context, repository verification.Repository) ([]byte, error)
	// FetchManifestAtRef returns the raw root ghd.toml for repository at ref.
	FetchManifestAtRef(ctx context.Context, repository verification.Repository, ref string) ([]byte, error)
}

// ReleaseAssetSource resolves release assets.
type ReleaseAssetSource interface {
	// ResolveReleaseAsset resolves a concrete release asset name for tag.
	ResolveReleaseAsset(
		ctx context.Context,
		repository verification.Repository,
		tag verification.ReleaseTag,
		assetName string,
	) (ReleaseAsset, error)
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

// DownloadFileSystem manages verified download staging and publication.
type DownloadFileSystem interface {
	// CreateDownloadDir creates a private temporary directory for untrusted download bytes.
	CreateDownloadDir(ctx context.Context) (string, func(), error)
	// PublishVerifiedArtifact copies a verified artifact into outputDir and returns its final path.
	PublishVerifiedArtifact(ctx context.Context, sourcePath string, outputDir string, assetName string) (string, error)
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

// VerifiedDownloadProgressStage identifies one user-visible download step.
type VerifiedDownloadProgressStage string

const (
	// VerifiedDownloadProgressResolvingManifest means ghd is resolving package metadata.
	VerifiedDownloadProgressResolvingManifest VerifiedDownloadProgressStage = "resolving-manifest"
	// VerifiedDownloadProgressResolvingAsset means ghd is selecting the concrete release asset.
	VerifiedDownloadProgressResolvingAsset VerifiedDownloadProgressStage = "resolving-asset"
	// VerifiedDownloadProgressDownloading means ghd is downloading the selected asset.
	VerifiedDownloadProgressDownloading VerifiedDownloadProgressStage = "downloading"
	// VerifiedDownloadProgressVerifying means ghd is verifying the downloaded asset.
	VerifiedDownloadProgressVerifying VerifiedDownloadProgressStage = "verifying"
	// VerifiedDownloadProgressWritingEvidence means ghd is writing verification evidence.
	VerifiedDownloadProgressWritingEvidence VerifiedDownloadProgressStage = "writing-evidence"
)

// VerifiedDownloadProgress describes user-visible verified download progress.
type VerifiedDownloadProgress struct {
	// Stage identifies the current lifecycle step.
	Stage VerifiedDownloadProgressStage
	// Message is a short user-facing description of the current step.
	Message string
	// Download carries byte-level download progress when Stage is downloading.
	Download *DownloadProgress
}

// VerifiedDownloadProgressFunc receives user-visible verified download progress.
type VerifiedDownloadProgressFunc func(VerifiedDownloadProgress)

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
	// FileSystem stages untrusted downloads and publishes verified artifacts.
	FileSystem DownloadFileSystem
}

// VerifiedDownloader implements the verified download use case.
type VerifiedDownloader struct {
	manifests ManifestSource
	assets    ReleaseAssetSource
	download  ArtifactDownloader
	verify    Verifier
	evidence  EvidenceWriter
	files     DownloadFileSystem
}

// VerifiedDownloadRequest describes one verified download.
type VerifiedDownloadRequest struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// Version is the literal version value used for manifest pattern expansion.
	Version manifest.PackageVersion
	// OutputDir receives the downloaded artifact and verification evidence.
	OutputDir string
	// Platform optionally overrides the current OS/architecture.
	Platform manifest.Platform
	// Progress receives user-visible verified download progress. Nil disables progress reports.
	Progress VerifiedDownloadProgressFunc
}

// VerifiedDownloadResult describes a completed verified download.
type VerifiedDownloadResult struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// Version is the literal requested version.
	Version manifest.PackageVersion
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
		return errors.New("verification repository must be owner/repo")
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
	tag, err := verification.NewReleaseTag(r.Tag)
	if err != nil {
		return fmt.Errorf("verification tag %q is invalid: %w", r.Tag, err)
	}
	if r.Evidence.Repository.IsZero() {
		return errors.New("verification evidence repository must be set")
	}
	if !strings.EqualFold(r.Evidence.Repository.String(), r.Repository) {
		return fmt.Errorf(
			"verification evidence repository %s does not match record repository %s",
			r.Evidence.Repository,
			r.Repository,
		)
	}
	if err := r.Evidence.Tag.Validate(); err != nil {
		return fmt.Errorf("verification evidence tag is invalid: %w", err)
	}
	if r.Evidence.Tag != tag {
		return fmt.Errorf("verification evidence tag %s does not match record tag %s", r.Evidence.Tag, r.Tag)
	}
	if r.Evidence.AssetDigest.IsZero() {
		return errors.New("verification evidence asset digest must be set")
	}
	return nil
}

// NewVerifiedDownloader creates a verified download use case.
func NewVerifiedDownloader(deps VerifiedDownloadDependencies) (*VerifiedDownloader, error) {
	if deps.Manifests == nil {
		return nil, errors.New("manifest source must be set")
	}
	if deps.Assets == nil {
		return nil, errors.New("release asset source must be set")
	}
	if deps.Downloader == nil {
		return nil, errors.New("artifact downloader must be set")
	}
	if deps.Verifier == nil {
		return nil, errors.New("verifier must be set")
	}
	if deps.EvidenceWriter == nil {
		return nil, errors.New("evidence writer must be set")
	}
	if deps.FileSystem == nil {
		return nil, errors.New("download filesystem must be set")
	}
	return &VerifiedDownloader{
		manifests: deps.Manifests,
		assets:    deps.Assets,
		download:  deps.Downloader,
		verify:    deps.Verifier,
		evidence:  deps.EvidenceWriter,
		files:     deps.FileSystem,
	}, nil
}

// Download fetches, verifies, and records one release asset.
//
//nolint:funlen // The use case intentionally reads as one audited verification workflow.
func (d *VerifiedDownloader) Download(
	ctx context.Context,
	request VerifiedDownloadRequest,
) (VerifiedDownloadResult, error) {
	if err := request.validate(); err != nil {
		return VerifiedDownloadResult{}, err
	}
	platform := request.Platform.WithDefaults()

	request.report(VerifiedDownloadProgressResolvingManifest, "Resolving package manifest")
	manifestBytes, err := d.manifests.FetchManifest(ctx, request.Repository)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	discoveryCfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	discoveryPkg, err := discoveryCfg.Package(request.PackageName)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	tag, err := discoveryPkg.ReleaseTag(request.Version)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	cfg, pkg, err := fetchPackageManifestForVersionAtTag(
		ctx,
		d.manifests,
		request.Repository,
		request.PackageName,
		request.Version,
		tag,
	)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	selected, err := pkg.SelectAsset(platform, request.Version)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	request.report(VerifiedDownloadProgressResolvingAsset, "Resolving release asset")
	releaseAsset, err := d.assets.ResolveReleaseAsset(ctx, request.Repository, tag, selected.Name)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("resolve release asset %q: %w", selected.Name, err)
	}
	downloadDir, cleanup, err := d.files.CreateDownloadDir(ctx)
	if err != nil {
		return VerifiedDownloadResult{}, err
	}
	defer cleanup()

	downloadRequest := DownloadReleaseAssetRequest{
		Asset:     releaseAsset,
		OutputDir: downloadDir,
	}
	if request.Progress != nil {
		downloadRequest.Progress = func(progress DownloadProgress) {
			request.reportDownload(progress)
		}
	}
	artifactPath, err := d.download.DownloadReleaseAsset(ctx, downloadRequest)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("download release asset %q: %w", releaseAsset.Name, err)
	}
	request.report(VerifiedDownloadProgressVerifying, "Verifying release artifact")
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
	publishedArtifactPath, err := d.files.PublishVerifiedArtifact(
		ctx,
		artifactPath,
		request.OutputDir,
		releaseAsset.Name,
	)
	if err != nil {
		return VerifiedDownloadResult{}, fmt.Errorf("publish verified artifact %q: %w", releaseAsset.Name, err)
	}

	record := VerificationRecord{
		SchemaVersion: 1,
		Repository:    request.Repository.String(),
		Package:       request.PackageName.String(),
		Version:       request.Version.String(),
		Tag:           string(tag),
		Asset:         selected.Name,
		Evidence:      evidence,
	}
	request.report(VerifiedDownloadProgressWritingEvidence, "Writing verification evidence")
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
		ArtifactPath: publishedArtifactPath,
		EvidencePath: evidencePath,
		Evidence:     evidence,
	}, nil
}

func (r VerifiedDownloadRequest) validate() error {
	if err := r.Repository.Validate(); err != nil {
		return err
	}
	if err := r.PackageName.Validate(); err != nil {
		return err
	}
	if err := r.Version.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.OutputDir) == "" {
		return errors.New("output directory must be set")
	}
	return nil
}

func (r VerifiedDownloadRequest) report(stage VerifiedDownloadProgressStage, message string) {
	if r.Progress == nil {
		return
	}
	r.Progress(VerifiedDownloadProgress{
		Stage:   stage,
		Message: message,
	})
}

func (r VerifiedDownloadRequest) reportDownload(progress DownloadProgress) {
	if r.Progress == nil {
		return
	}
	copied := progress
	r.Progress(VerifiedDownloadProgress{
		Stage:    VerifiedDownloadProgressDownloading,
		Message:  "Downloading release asset",
		Download: &copied,
	})
}

func fetchPackageManifestForVersionAtTag(
	ctx context.Context,
	manifests ManifestSource,
	repository verification.Repository,
	packageName manifest.PackageName,
	version manifest.PackageVersion,
	tag verification.ReleaseTag,
) (manifest.Config, manifest.Package, error) {
	manifestBytes, err := manifests.FetchManifestAtRef(ctx, repository, string(tag))
	if err != nil {
		return manifest.Config{}, manifest.Package{}, fmt.Errorf("fetch ghd.toml at %s: %w", tag, err)
	}
	cfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return manifest.Config{}, manifest.Package{}, err
	}
	pkg, err := cfg.Package(packageName)
	if err != nil {
		return manifest.Config{}, manifest.Package{}, err
	}
	resolvedTag, err := pkg.ReleaseTag(version)
	if err != nil {
		return manifest.Config{}, manifest.Package{}, err
	}
	if resolvedTag != tag {
		return manifest.Config{}, manifest.Package{}, fmt.Errorf(
			"ghd.toml at %s maps %s@%s to %s",
			tag,
			packageName,
			version,
			resolvedTag,
		)
	}
	return cfg, pkg, nil
}
