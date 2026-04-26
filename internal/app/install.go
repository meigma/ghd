package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

// ArtifactMaterializer prepares configured binaries from a verified artifact.
type ArtifactMaterializer interface {
	// MaterializeBinaries prepares request.ArtifactPath into request.DestinationDir.
	MaterializeBinaries(ctx context.Context, request ArtifactMaterializationRequest) ([]MaterializedBinary, error)
}

// InstallFileSystem owns install-time filesystem state and links.
type InstallFileSystem interface {
	// CreateDownloadDir creates a temporary directory for non-executable downloads.
	CreateDownloadDir(ctx context.Context) (string, func(), error)
	// CreateStoreLayout creates the digest-keyed store layout and copies the artifact.
	CreateStoreLayout(ctx context.Context, request StoreLayoutRequest) (StoreLayout, error)
	// LinkBinaries links prepared binaries into the managed bin directory.
	LinkBinaries(ctx context.Context, request LinkBinariesRequest) ([]InstalledBinary, error)
	// RemoveManagedInstall removes managed binaries and store contents for one install.
	RemoveManagedInstall(ctx context.Context, request RemoveManagedInstallRequest) error
	// WriteInstallMetadata writes install metadata into storePath.
	WriteInstallMetadata(ctx context.Context, storePath string, record InstallRecord) (string, error)
}

// InstalledStateStore persists active installed package state.
type InstalledStateStore interface {
	// LoadInstalledState reads active installed package state from stateDir.
	LoadInstalledState(ctx context.Context, stateDir string) (state.Index, error)
	// AddInstalledRecord adds an active installed package record to stateDir.
	AddInstalledRecord(ctx context.Context, stateDir string, record state.Record) (state.Index, error)
}

// VerifiedInstallDependencies contains the ports needed by VerifiedInstaller.
type VerifiedInstallDependencies struct {
	// Manifests fetches repository manifest bytes.
	Manifests ManifestSource
	// Releases lists repository releases for latest-version resolution.
	Releases RepositoryReleaseSource
	// Assets resolves concrete release assets.
	Assets ReleaseAssetSource
	// Downloader downloads concrete release assets.
	Downloader ArtifactDownloader
	// Verifier verifies downloaded assets.
	Verifier Verifier
	// EvidenceWriter records verification evidence.
	EvidenceWriter EvidenceWriter
	// Materializer prepares configured binaries from verified artifacts.
	Materializer ArtifactMaterializer
	// FileSystem owns install store and binary exposure behavior.
	FileSystem InstallFileSystem
	// StateStore persists active installed package records.
	StateStore InstalledStateStore
	// Now returns the current time for installed records.
	Now func() time.Time
}

// VerifiedInstaller implements the verified install use case.
type VerifiedInstaller struct {
	manifests    ManifestSource
	releases     RepositoryReleaseSource
	assets       ReleaseAssetSource
	download     ArtifactDownloader
	verify       Verifier
	evidence     EvidenceWriter
	materializer ArtifactMaterializer
	files        InstallFileSystem
	state        InstalledStateStore
	now          func() time.Time
}

// ErrInstallNotApproved means installation stopped because the verified artifact was not approved.
var ErrInstallNotApproved = errors.New("install was not approved")

// InstallProgressStage identifies one user-visible install step.
type InstallProgressStage string

const (
	// InstallProgressCheckingState means install is reading active installed package state.
	InstallProgressCheckingState InstallProgressStage = "checking-state"
	// InstallProgressFetchingManifest means install is fetching repository manifest data.
	InstallProgressFetchingManifest InstallProgressStage = "fetching-manifest"
	// InstallProgressResolvingPackage means install is selecting package, version, and platform asset metadata.
	InstallProgressResolvingPackage InstallProgressStage = "resolving-package"
	// InstallProgressResolvingAsset means install is resolving the concrete GitHub release asset.
	InstallProgressResolvingAsset InstallProgressStage = "resolving-asset"
	// InstallProgressPreparingDownload means install is preparing temporary download storage.
	InstallProgressPreparingDownload InstallProgressStage = "preparing-download"
	// InstallProgressDownloading means install is downloading the selected release asset.
	InstallProgressDownloading InstallProgressStage = "downloading"
	// InstallProgressVerifying means install is verifying release and provenance attestations.
	InstallProgressVerifying InstallProgressStage = "verifying"
	// InstallProgressAwaitingApproval means install has verified the asset and is waiting for approval.
	InstallProgressAwaitingApproval InstallProgressStage = "awaiting-approval"
	// InstallProgressPreparingStore means install is creating the managed store layout.
	InstallProgressPreparingStore InstallProgressStage = "preparing-store"
	// InstallProgressExtracting means install is preparing configured binaries.
	InstallProgressExtracting InstallProgressStage = "extracting"
	// InstallProgressWritingEvidence means install is writing verification evidence.
	InstallProgressWritingEvidence InstallProgressStage = "writing-evidence"
	// InstallProgressLinkingBinaries means install is exposing binaries in the managed bin directory.
	InstallProgressLinkingBinaries InstallProgressStage = "linking-binaries"
	// InstallProgressWritingMetadata means install is writing package install metadata.
	InstallProgressWritingMetadata InstallProgressStage = "writing-metadata"
	// InstallProgressRecordingState means install is recording active installed package state.
	InstallProgressRecordingState InstallProgressStage = "recording-state"
)

// InstallProgress describes one user-visible install step.
type InstallProgress struct {
	// Stage identifies the step underway.
	Stage InstallProgressStage
	// Message is a concise human-readable status line.
	Message string
	// Download carries byte-level download progress when Stage is InstallProgressDownloading.
	Download *DownloadProgress
}

// InstallProgressFunc receives user-visible install progress.
type InstallProgressFunc func(InstallProgress)

// InstallApproval contains verified artifact facts for the install approval decision.
type InstallApproval struct {
	// Repository is the verified GitHub repository.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// Version is the requested package version.
	Version manifest.PackageVersion
	// Tag is the resolved GitHub release tag.
	Tag verification.ReleaseTag
	// AssetName is the concrete release asset name.
	AssetName string
	// AssetDigest is the verified local asset digest.
	AssetDigest verification.Digest
	// ReleasePredicateType is the accepted immutable release predicate type.
	ReleasePredicateType string
	// ProvenancePredicateType is the accepted provenance predicate type.
	ProvenancePredicateType string
	// SignerWorkflow is the accepted provenance signer workflow.
	SignerWorkflow verification.WorkflowIdentity
	// TrustRootPath is the custom Sigstore trusted_root.json path, when configured.
	TrustRootPath string
	// BinDir receives exposed binary links if the install proceeds.
	BinDir string
	// Binaries are the binary names that will be exposed if the install proceeds.
	Binaries []string
}

// InstallApprovalFunc approves or rejects a verified install before local mutation.
type InstallApprovalFunc func(context.Context, InstallApproval) error

// VerifiedInstallRequest describes one verified install.
type VerifiedInstallRequest struct {
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// Version is the literal version value used for manifest pattern expansion.
	// Zero means install the latest eligible stable release.
	Version manifest.PackageVersion
	// StoreDir is the root of ghd's managed package store.
	StoreDir string
	// BinDir receives links to installed binaries.
	BinDir string
	// StateDir stores active installed package state.
	StateDir string
	// TrustRootPath is the custom Sigstore trusted_root.json path, when configured.
	TrustRootPath string
	// Platform optionally overrides the current OS/architecture.
	Platform manifest.Platform
	// Progress receives user-visible install progress. Nil disables progress reports.
	Progress InstallProgressFunc
	// Approve approves a verified artifact before preparing binaries, linking, or state writes. Nil approves automatically.
	Approve InstallApprovalFunc
}

// VerifiedInstallResult describes a completed verified install.
type VerifiedInstallResult struct {
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
	// StorePath is the digest-keyed store directory.
	StorePath string
	// ArtifactPath is the stored artifact path.
	ArtifactPath string
	// ExtractedPath is the prepared binaries directory inside the store.
	ExtractedPath string
	// EvidencePath is the local verification evidence path.
	EvidencePath string
	// MetadataPath is the local install metadata path.
	MetadataPath string
	// Binaries are the installed binary links.
	Binaries []InstalledBinary
	// Evidence is the accepted release and provenance verification evidence.
	Evidence verification.Evidence
	// TrustRootPath is the custom Sigstore trusted_root.json path, when configured.
	TrustRootPath string
}

// ArtifactMaterializationRequest describes one verified artifact preparation.
type ArtifactMaterializationRequest struct {
	// ArtifactPath is the verified stored artifact to materialize.
	ArtifactPath string
	// AssetName is the original release asset name used for type detection.
	AssetName string
	// DestinationDir receives prepared files.
	DestinationDir string
	// Binaries are the configured executable paths expected after preparation.
	Binaries []manifest.Binary
}

// MaterializedBinary describes one configured executable prepared from a verified artifact.
type MaterializedBinary struct {
	// Name is the exposed command name.
	Name string `json:"name"`
	// RelativePath is the configured path inside the prepared binaries directory.
	RelativePath string `json:"relative_path"`
	// Path is the absolute prepared binary path.
	Path string `json:"path"`
}

// StoreLayoutRequest describes one digest-keyed store layout.
type StoreLayoutRequest struct {
	// StoreRoot is the managed store root.
	StoreRoot string
	// Repository is the GitHub repository that owns the package.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// Version is the literal requested version.
	Version manifest.PackageVersion
	// AssetDigest is the verified local artifact digest.
	AssetDigest verification.Digest
	// ArtifactPath is the verified temporary artifact path.
	ArtifactPath string
}

// StoreLayout describes the filesystem paths for one installed artifact.
type StoreLayout struct {
	// StorePath is the digest-keyed store directory.
	StorePath string
	// ArtifactPath is the copied artifact path inside StorePath.
	ArtifactPath string
	// ExtractedDir is the prepared binaries destination inside StorePath.
	ExtractedDir string
}

// LinkBinariesRequest describes binary links to create.
type LinkBinariesRequest struct {
	// BinDir is the managed binary link directory.
	BinDir string
	// Binaries are the prepared binaries to expose.
	Binaries []MaterializedBinary
}

// RemoveManagedInstallRequest describes managed filesystem state to remove.
type RemoveManagedInstallRequest struct {
	// StoreRoot is the managed package store root.
	StoreRoot string
	// BinRoot is the managed binary link directory.
	BinRoot string
	// StorePath is the managed digest-keyed store directory.
	StorePath string
	// Binaries are the recorded binary links to remove.
	Binaries []InstalledBinary
}

// InstalledBinary describes one exposed binary link.
type InstalledBinary struct {
	// Name is the exposed command name.
	Name string `json:"name"`
	// LinkPath is the managed bin path.
	LinkPath string `json:"link_path"`
	// TargetPath is the verified prepared binary path.
	TargetPath string `json:"target_path"`
}

// InstallRecord is the JSON record written after install succeeds.
type InstallRecord struct {
	// SchemaVersion is the install record schema version.
	SchemaVersion int `json:"schema_version"`
	// Repository is the installed GitHub repository.
	Repository string `json:"repository"`
	// Package is the installed package name.
	Package string `json:"package"`
	// Version is the requested package version.
	Version string `json:"version"`
	// Tag is the resolved release tag.
	Tag string `json:"tag"`
	// Asset is the verified release asset name.
	Asset string `json:"asset"`
	// AssetDigest is the verified artifact digest.
	AssetDigest string `json:"asset_digest"`
	// StorePath is the digest-keyed store directory.
	StorePath string `json:"store_path"`
	// ArtifactPath is the copied artifact path inside the store.
	ArtifactPath string `json:"artifact_path"`
	// ExtractedPath is the prepared binaries directory inside the store.
	ExtractedPath string `json:"extracted_path"`
	// VerificationPath is the verification evidence path.
	VerificationPath string `json:"verification_path"`
	// Binaries are the exposed installed binaries.
	Binaries []InstalledBinary `json:"binaries"`
}

// NewVerifiedInstaller creates a verified install use case.
func NewVerifiedInstaller(deps VerifiedInstallDependencies) (*VerifiedInstaller, error) {
	if deps.Manifests == nil {
		return nil, errors.New("manifest source must be set")
	}
	if deps.Releases == nil {
		return nil, errors.New("release source must be set")
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
	if deps.Materializer == nil {
		return nil, errors.New("artifact materializer must be set")
	}
	if deps.FileSystem == nil {
		return nil, errors.New("install filesystem must be set")
	}
	if deps.StateStore == nil {
		return nil, errors.New("installed state store must be set")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &VerifiedInstaller{
		manifests:    deps.Manifests,
		releases:     deps.Releases,
		assets:       deps.Assets,
		download:     deps.Downloader,
		verify:       deps.Verifier,
		evidence:     deps.EvidenceWriter,
		materializer: deps.Materializer,
		files:        deps.FileSystem,
		state:        deps.StateStore,
		now:          now,
	}, nil
}

func (i *VerifiedInstaller) resolveRelease(
	ctx context.Context,
	request VerifiedInstallRequest,
	platform manifest.Platform,
) (latestStablePackageRelease, error) {
	manifestBytes, err := i.manifests.FetchManifest(ctx, request.Repository)
	if err != nil {
		return latestStablePackageRelease{}, fmt.Errorf("fetch ghd.toml: %w", err)
	}
	discoveryCfg, err := manifest.Decode(manifestBytes)
	if err != nil {
		return latestStablePackageRelease{}, err
	}
	discoveryPkg, err := discoveryCfg.Package(request.PackageName)
	if err != nil {
		return latestStablePackageRelease{}, err
	}
	if !request.Version.IsZero() {
		return resolveReleaseForVersion(
			ctx,
			i.manifests,
			request.Repository,
			request.PackageName,
			request.Version,
			discoveryPkg,
			platform,
		)
	}

	repositoryReleases, err := i.releases.ListRepositoryReleases(ctx, request.Repository)
	if err != nil {
		return latestStablePackageRelease{}, fmt.Errorf("list GitHub releases: %w", err)
	}
	candidate, err := latestStablePackageReleaseForPlatform(
		ctx,
		i.manifests,
		request.Repository,
		request.PackageName,
		repositoryReleases,
		platform,
		"",
	)
	if err != nil {
		return latestStablePackageRelease{}, err
	}
	if candidate.Version.IsZero() {
		return latestStablePackageRelease{}, fmt.Errorf(
			"no eligible stable release found for %s/%s on %s/%s",
			request.Repository,
			request.PackageName,
			platform.OS,
			platform.Arch,
		)
	}
	return candidate, nil
}

func resolveReleaseForVersion(
	ctx context.Context,
	manifests ManifestSource,
	repository verification.Repository,
	packageName manifest.PackageName,
	version manifest.PackageVersion,
	discoveryPkg manifest.Package,
	platform manifest.Platform,
) (latestStablePackageRelease, error) {
	tag, err := discoveryPkg.ReleaseTag(version)
	if err != nil {
		return latestStablePackageRelease{}, err
	}
	cfg, pkg, err := fetchPackageManifestForVersionAtTag(ctx, manifests, repository, packageName, version, tag)
	if err != nil {
		return latestStablePackageRelease{}, err
	}
	asset, assetName, err := resolvedAssetForPlatform(pkg, version, platform)
	if err != nil {
		return latestStablePackageRelease{}, err
	}
	return latestStablePackageRelease{
		Config:    cfg,
		Package:   pkg,
		Version:   version,
		Tag:       tag,
		Asset:     asset,
		AssetName: assetName,
	}, nil
}

// Install fetches, verifies, extracts, links, and records one package install.
//
//nolint:funlen // The install workflow is intentionally linear so rollback and progress order remain visible.
func (i *VerifiedInstaller) Install(
	ctx context.Context,
	request VerifiedInstallRequest,
) (VerifiedInstallResult, error) {
	if err := request.validate(); err != nil {
		return VerifiedInstallResult{}, err
	}
	platform := request.Platform.WithDefaults()
	request.report(InstallProgressCheckingState, "Checking installed packages")
	installedState, err := i.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return VerifiedInstallResult{}, err
	}
	if _, ok := installedState.Record(request.Repository.String(), request.PackageName.String()); ok {
		return VerifiedInstallResult{}, state.DuplicateInstallError{
			Repository: request.Repository.String(),
			Package:    request.PackageName.String(),
		}
	}

	request.report(InstallProgressFetchingManifest, "Fetching ghd.toml")
	request.report(InstallProgressResolvingPackage, "Resolving package and platform asset")
	resolved, err := i.resolveRelease(ctx, request, platform)
	if err != nil {
		return VerifiedInstallResult{}, err
	}
	if ownershipErr := installedState.CheckBinaryOwnership(state.PackageRef{
		Repository: request.Repository.String(),
		Package:    request.PackageName.String(),
	}, manifestBinaryNames(resolved.Package.Binaries), state.PackageRef{}); ownershipErr != nil {
		return VerifiedInstallResult{}, ownershipErr
	}
	request.report(InstallProgressResolvingAsset, "Resolving GitHub release asset")
	releaseAsset, err := i.assets.ResolveReleaseAsset(ctx, request.Repository, resolved.Tag, resolved.AssetName)
	if err != nil {
		return VerifiedInstallResult{}, fmt.Errorf("resolve release asset %q: %w", resolved.AssetName, err)
	}

	request.report(InstallProgressPreparingDownload, "Preparing download")
	downloadDir, cleanup, err := i.files.CreateDownloadDir(ctx)
	if err != nil {
		return VerifiedInstallResult{}, err
	}
	defer cleanup()

	request.report(InstallProgressDownloading, fmt.Sprintf("Downloading %s", releaseAsset.Name))
	downloadRequest := DownloadReleaseAssetRequest{
		Asset:     releaseAsset,
		OutputDir: downloadDir,
	}
	if request.Progress != nil {
		downloadRequest.Progress = func(progress DownloadProgress) {
			request.reportDownload(progress)
		}
	}
	artifactPath, err := i.download.DownloadReleaseAsset(ctx, downloadRequest)
	if err != nil {
		return VerifiedInstallResult{}, fmt.Errorf("download release asset %q: %w", releaseAsset.Name, err)
	}
	request.report(InstallProgressVerifying, "Verifying release and provenance")
	evidence, err := i.verify.VerifyReleaseAsset(ctx, verification.Request{
		Repository: request.Repository,
		Tag:        resolved.Tag,
		AssetPath:  artifactPath,
		Policy: verification.Policy{
			TrustedSignerWorkflow: resolved.Config.Provenance.TrustedSignerWorkflow(),
		},
	})
	if err != nil {
		return VerifiedInstallResult{}, err
	}

	request.report(InstallProgressAwaitingApproval, "Reviewing verified install")
	if approvalErr := request.approve(ctx, InstallApproval{
		Repository:              request.Repository,
		PackageName:             request.PackageName,
		Version:                 resolved.Version,
		Tag:                     resolved.Tag,
		AssetName:               resolved.AssetName,
		AssetDigest:             evidence.AssetDigest,
		ReleasePredicateType:    evidence.ReleaseAttestation.PredicateType,
		ProvenancePredicateType: evidence.ProvenanceAttestation.PredicateType,
		SignerWorkflow:          evidence.ProvenanceAttestation.SignerWorkflow,
		TrustRootPath:           request.TrustRootPath,
		BinDir:                  request.BinDir,
		Binaries:                manifestBinaryNames(resolved.Package.Binaries),
	}); approvalErr != nil {
		return VerifiedInstallResult{}, approvalErr
	}

	request.report(InstallProgressPreparingStore, "Preparing managed store")
	layout, err := i.files.CreateStoreLayout(ctx, StoreLayoutRequest{
		StoreRoot:    request.StoreDir,
		Repository:   request.Repository,
		PackageName:  request.PackageName,
		Version:      resolved.Version,
		AssetDigest:  evidence.AssetDigest,
		ArtifactPath: artifactPath,
	})
	if err != nil {
		return VerifiedInstallResult{}, err
	}

	request.report(InstallProgressExtracting, "Preparing configured binaries")
	materialized, err := i.materializer.MaterializeBinaries(ctx, ArtifactMaterializationRequest{
		ArtifactPath:   layout.ArtifactPath,
		AssetName:      resolved.AssetName,
		DestinationDir: layout.ExtractedDir,
		Binaries:       resolved.Package.Binaries,
	})
	if err != nil {
		return VerifiedInstallResult{}, i.cleanupManagedInstall(ctx, RemoveManagedInstallRequest{
			StoreRoot: request.StoreDir,
			BinRoot:   request.BinDir,
			StorePath: layout.StorePath,
		}, err)
	}

	verificationRecord := VerificationRecord{
		SchemaVersion: 1,
		Repository:    request.Repository.String(),
		Package:       request.PackageName.String(),
		Version:       resolved.Version.String(),
		Tag:           string(resolved.Tag),
		Asset:         resolved.AssetName,
		Evidence:      evidence,
	}
	request.report(InstallProgressWritingEvidence, "Writing verification evidence")
	evidencePath, err := i.evidence.WriteVerificationEvidence(ctx, layout.StorePath, verificationRecord)
	if err != nil {
		return VerifiedInstallResult{}, i.cleanupManagedInstall(ctx, RemoveManagedInstallRequest{
			StoreRoot: request.StoreDir,
			BinRoot:   request.BinDir,
			StorePath: layout.StorePath,
		}, fmt.Errorf("write verification evidence: %w", err))
	}

	request.report(InstallProgressLinkingBinaries, "Linking binaries")
	links, err := i.files.LinkBinaries(ctx, LinkBinariesRequest{
		BinDir:   request.BinDir,
		Binaries: materialized,
	})
	if err != nil {
		return VerifiedInstallResult{}, i.cleanupManagedInstall(ctx, RemoveManagedInstallRequest{
			StoreRoot: request.StoreDir,
			BinRoot:   request.BinDir,
			StorePath: layout.StorePath,
		}, err)
	}

	installRecord := InstallRecord{
		SchemaVersion:    1,
		Repository:       request.Repository.String(),
		Package:          request.PackageName.String(),
		Version:          resolved.Version.String(),
		Tag:              string(resolved.Tag),
		Asset:            resolved.AssetName,
		AssetDigest:      evidence.AssetDigest.String(),
		StorePath:        layout.StorePath,
		ArtifactPath:     layout.ArtifactPath,
		ExtractedPath:    layout.ExtractedDir,
		VerificationPath: evidencePath,
		Binaries:         links,
	}
	request.report(InstallProgressWritingMetadata, "Writing install metadata")
	metadataPath, err := i.files.WriteInstallMetadata(ctx, layout.StorePath, installRecord)
	if err != nil {
		return VerifiedInstallResult{}, i.cleanupManagedInstall(ctx, RemoveManagedInstallRequest{
			StoreRoot: request.StoreDir,
			BinRoot:   request.BinDir,
			StorePath: layout.StorePath,
			Binaries:  links,
		}, fmt.Errorf("write install metadata: %w", err))
	}
	record := state.Record{
		Repository:       installRecord.Repository,
		Package:          installRecord.Package,
		Version:          installRecord.Version,
		Tag:              installRecord.Tag,
		Asset:            installRecord.Asset,
		AssetDigest:      installRecord.AssetDigest,
		StorePath:        installRecord.StorePath,
		ArtifactPath:     installRecord.ArtifactPath,
		ExtractedPath:    installRecord.ExtractedPath,
		VerificationPath: installRecord.VerificationPath,
		Binaries:         stateBinaries(links),
		InstalledAt:      i.now().UTC(),
	}
	request.report(InstallProgressRecordingState, "Recording installed state")
	if _, err := i.state.AddInstalledRecord(ctx, request.StateDir, record); err != nil {
		return VerifiedInstallResult{}, i.cleanupManagedInstall(ctx, RemoveManagedInstallRequest{
			StoreRoot: request.StoreDir,
			BinRoot:   request.BinDir,
			StorePath: layout.StorePath,
			Binaries:  links,
		}, fmt.Errorf("record installed state: %w", err))
	}

	return VerifiedInstallResult{
		Repository:    request.Repository,
		PackageName:   request.PackageName,
		Version:       resolved.Version,
		Tag:           resolved.Tag,
		AssetName:     resolved.AssetName,
		StorePath:     layout.StorePath,
		ArtifactPath:  layout.ArtifactPath,
		ExtractedPath: layout.ExtractedDir,
		EvidencePath:  evidencePath,
		MetadataPath:  metadataPath,
		Binaries:      links,
		Evidence:      evidence,
		TrustRootPath: request.TrustRootPath,
	}, nil
}

func (i *VerifiedInstaller) cleanupManagedInstall(
	ctx context.Context,
	request RemoveManagedInstallRequest,
	err error,
) error {
	if cleanupErr := i.files.RemoveManagedInstall(context.WithoutCancel(ctx), request); cleanupErr != nil {
		return errors.Join(err, fmt.Errorf("cleanup managed install: %w", cleanupErr))
	}
	return err
}

func (r VerifiedInstallRequest) report(stage InstallProgressStage, message string) {
	if r.Progress == nil {
		return
	}
	r.Progress(InstallProgress{
		Stage:   stage,
		Message: message,
	})
}

func (r VerifiedInstallRequest) reportDownload(progress DownloadProgress) {
	if r.Progress == nil {
		return
	}
	copied := progress
	message := "Downloading"
	if strings.TrimSpace(copied.AssetName) != "" {
		message = fmt.Sprintf("Downloading %s", copied.AssetName)
	}
	r.Progress(InstallProgress{
		Stage:    InstallProgressDownloading,
		Message:  message,
		Download: &copied,
	})
}

func (r VerifiedInstallRequest) approve(ctx context.Context, approval InstallApproval) error {
	if r.Approve == nil {
		return nil
	}
	return r.Approve(ctx, approval)
}

func (r VerifiedInstallRequest) validate() error {
	if err := r.Repository.Validate(); err != nil {
		return err
	}
	if err := r.PackageName.Validate(); err != nil {
		return err
	}
	if !r.Version.IsZero() {
		if err := r.Version.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(r.StoreDir) == "" {
		return errors.New("store directory must be set")
	}
	if strings.TrimSpace(r.BinDir) == "" {
		return errors.New("bin directory must be set")
	}
	if strings.TrimSpace(r.StateDir) == "" {
		return errors.New("state directory must be set")
	}
	return nil
}

func stateBinaries(binaries []InstalledBinary) []state.Binary {
	records := make([]state.Binary, 0, len(binaries))
	for _, binary := range binaries {
		records = append(records, state.Binary{
			Name:       binary.Name,
			LinkPath:   binary.LinkPath,
			TargetPath: binary.TargetPath,
		})
	}
	return records
}
