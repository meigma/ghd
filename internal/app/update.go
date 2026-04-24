package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

// ReplaceManagedBinariesRequest describes one managed-link swap.
type ReplaceManagedBinariesRequest struct {
	// BinDir is the managed binary link directory.
	BinDir string
	// Previous are the active managed binary links to replace.
	Previous []InstalledBinary
	// Next are the new managed binary links to activate.
	Next []InstalledBinary
}

// UpdateFileSystem owns update-time filesystem staging and swap behavior.
type UpdateFileSystem interface {
	// CreateDownloadDir creates a temporary directory for non-executable downloads.
	CreateDownloadDir(ctx context.Context) (string, func(), error)
	// CreateStoreLayout creates the digest-keyed store layout and copies the artifact.
	CreateStoreLayout(ctx context.Context, request StoreLayoutRequest) (StoreLayout, error)
	// ReplaceManagedBinaries swaps one active binary set for another.
	ReplaceManagedBinaries(ctx context.Context, request ReplaceManagedBinariesRequest) error
	// RemoveManagedInstall removes managed binaries and store contents for one staged install.
	RemoveManagedInstall(ctx context.Context, request RemoveManagedInstallRequest) error
	// RemoveManagedStore removes only the managed store directory for one install.
	RemoveManagedStore(ctx context.Context, storeRoot string, storePath string) error
	// WriteInstallMetadata writes install metadata into storePath.
	WriteInstallMetadata(ctx context.Context, storePath string, record InstallRecord) (string, error)
}

// InstalledStateReplaceStore persists active installed package replacement.
type InstalledStateReplaceStore interface {
	// LoadInstalledState reads active installed package state from stateDir.
	LoadInstalledState(ctx context.Context, stateDir string) (state.Index, error)
	// ReplaceInstalledRecord replaces one active installed package record in stateDir.
	ReplaceInstalledRecord(ctx context.Context, stateDir string, record state.Record) (state.Index, error)
}

// PackageUpdaterDependencies contains the ports needed by PackageUpdater.
type PackageUpdaterDependencies struct {
	// Manifests fetches repository manifest bytes.
	Manifests ManifestSource
	// Releases lists repository releases for update discovery.
	Releases RepositoryReleaseSource
	// Assets resolves concrete release assets.
	Assets ReleaseAssetSource
	// Downloader downloads concrete release assets.
	Downloader ArtifactDownloader
	// Verifier verifies downloaded assets.
	Verifier Verifier
	// EvidenceWriter records verification evidence.
	EvidenceWriter EvidenceWriter
	// Archives extracts verified archives.
	Archives ArchiveExtractor
	// FileSystem owns install store and binary exposure behavior.
	FileSystem UpdateFileSystem
	// StateStore persists active installed package records.
	StateStore InstalledStateReplaceStore
	// Now returns the current time for installed records.
	Now func() time.Time
}

// UpdateStatus is one installed-package update outcome.
type UpdateStatus string

const (
	// UpdateStatusUpdated reports that a newer version became active.
	UpdateStatusUpdated UpdateStatus = "updated"
	// UpdateStatusUpdatedWithWarning reports that a newer version became active but post-success cleanup failed.
	UpdateStatusUpdatedWithWarning UpdateStatus = "updated-with-warning"
	// UpdateStatusAlreadyUpToDate reports that no newer eligible version exists.
	UpdateStatusAlreadyUpToDate UpdateStatus = "already-up-to-date"
	// UpdateStatusCannotUpdate reports that the package could not be updated.
	UpdateStatusCannotUpdate UpdateStatus = "cannot-update"
)

// UpdateInstalledResult is one installed-package update result row.
type UpdateInstalledResult struct {
	// Repository is the GitHub repository that owns the package.
	Repository string
	// Package is the installed package name.
	Package string
	// PreviousVersion is the version that was active when the update started.
	PreviousVersion string
	// CurrentVersion is the version active after the update attempt completes.
	CurrentVersion string
	// Status is the update outcome.
	Status UpdateStatus
	// Reason explains warnings or failures.
	Reason string
}

// UpdateIncompleteError reports one or more warning or failure rows.
type UpdateIncompleteError struct {
	// Failed is the number of packages that could not be updated.
	Failed int
	// Warned is the number of packages that updated with warnings.
	Warned int
}

// Error describes the aggregated batch update outcome.
func (e UpdateIncompleteError) Error() string {
	switch {
	case e.Warned == 0 && e.Failed == 1:
		return "could not update 1 installed package"
	case e.Warned == 0:
		return fmt.Sprintf("could not update %d installed packages", e.Failed)
	case e.Failed == 0 && e.Warned == 1:
		return "updated 1 installed package with warnings"
	case e.Failed == 0:
		return fmt.Sprintf("updated %d installed packages with warnings", e.Warned)
	case e.Warned == 1 && e.Failed == 1:
		return "update completed with 1 warning and 1 failure"
	case e.Warned > 1 && e.Failed == 1:
		return fmt.Sprintf("update completed with %d warnings and 1 failure", e.Warned)
	case e.Warned == 1:
		return fmt.Sprintf("update completed with 1 warning and %d failures", e.Failed)
	default:
		return fmt.Sprintf("update completed with %d warnings and %d failures", e.Warned, e.Failed)
	}
}

// PackageUpdater implements installed package updates.
type PackageUpdater struct {
	manifests ManifestSource
	releases  RepositoryReleaseSource
	assets    ReleaseAssetSource
	download  ArtifactDownloader
	verify    Verifier
	evidence  EvidenceWriter
	archives  ArchiveExtractor
	files     UpdateFileSystem
	state     InstalledStateReplaceStore
	now       func() time.Time
}

// UpdateRequest describes installed-package update requests.
type UpdateRequest struct {
	// Target is a package name, binary name, or owner/repo/package.
	Target string
	// All updates every active installed package.
	All bool
	// StoreDir is the root of ghd's managed package store.
	StoreDir string
	// BinDir is the managed binary link directory.
	BinDir string
	// StateDir stores active installed package state.
	StateDir string
}

type updateRecordResult struct {
	Previous state.Record
	Current  state.Record
	Updated  bool
}

// NewPackageUpdater creates a package updater use case.
func NewPackageUpdater(deps PackageUpdaterDependencies) (*PackageUpdater, error) {
	if deps.Manifests == nil {
		return nil, fmt.Errorf("manifest source must be set")
	}
	if deps.Releases == nil {
		return nil, fmt.Errorf("release source must be set")
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
	if deps.Archives == nil {
		return nil, fmt.Errorf("archive extractor must be set")
	}
	if deps.FileSystem == nil {
		return nil, fmt.Errorf("update filesystem must be set")
	}
	if deps.StateStore == nil {
		return nil, fmt.Errorf("installed state store must be set")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &PackageUpdater{
		manifests: deps.Manifests,
		releases:  deps.Releases,
		assets:    deps.Assets,
		download:  deps.Downloader,
		verify:    deps.Verifier,
		evidence:  deps.EvidenceWriter,
		archives:  deps.Archives,
		files:     deps.FileSystem,
		state:     deps.StateStore,
		now:       now,
	}, nil
}

// Update updates selected active installed packages.
func (u *PackageUpdater) Update(ctx context.Context, request UpdateRequest) ([]UpdateInstalledResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	index, err := u.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return nil, err
	}
	records, err := checkTargets(index.Normalize(), CheckRequest{
		Target: request.Target,
		All:    request.All,
	})
	if err != nil {
		return nil, err
	}

	results := make([]UpdateInstalledResult, 0, len(records))
	failed := 0
	warned := 0
	for _, previous := range records {
		recordResult, err := u.updateRecord(ctx, request, previous)
		row := updateRow(recordResult, err)
		results = append(results, row)
		switch row.Status {
		case UpdateStatusCannotUpdate:
			failed++
		case UpdateStatusUpdatedWithWarning:
			warned++
		}
	}
	if failed != 0 || warned != 0 {
		return results, UpdateIncompleteError{Failed: failed, Warned: warned}
	}
	return results, nil
}

func (u *PackageUpdater) updateRecord(ctx context.Context, request UpdateRequest, previous state.Record) (updateRecordResult, error) {
	result := updateRecordResult{
		Previous: previous,
		Current:  previous,
	}

	candidate, err := resolveInstalledPackageUpdate(ctx, u.manifests, u.releases, previous)
	if err != nil {
		return result, err
	}
	if candidate.LatestVersion == "" {
		return result, nil
	}
	if err := u.checkBinaryOwnership(ctx, request.StateDir, previous, candidate.Package.Binaries); err != nil {
		return result, err
	}

	tag, err := candidate.Package.ReleaseTag(candidate.LatestVersion)
	if err != nil {
		return result, err
	}
	assetName, err := candidate.InstalledAsset.ResolveName(candidate.LatestVersion)
	if err != nil {
		return result, err
	}
	releaseAsset, err := u.assets.ResolveReleaseAsset(ctx, candidate.Repository, tag, assetName)
	if err != nil {
		return result, fmt.Errorf("resolve release asset %q: %w", assetName, err)
	}

	downloadDir, cleanup, err := u.files.CreateDownloadDir(ctx)
	if err != nil {
		return result, err
	}
	defer cleanup()

	artifactPath, err := u.download.DownloadReleaseAsset(ctx, DownloadReleaseAssetRequest{
		Asset:     releaseAsset,
		OutputDir: downloadDir,
	})
	if err != nil {
		return result, fmt.Errorf("download release asset %q: %w", releaseAsset.Name, err)
	}
	evidence, err := u.verify.VerifyReleaseAsset(ctx, verification.Request{
		Repository: candidate.Repository,
		Tag:        tag,
		AssetPath:  artifactPath,
		Policy: verification.Policy{
			TrustedSignerWorkflow: candidate.Config.Provenance.TrustedSignerWorkflow(),
		},
	})
	if err != nil {
		return result, err
	}

	layout, err := u.files.CreateStoreLayout(ctx, StoreLayoutRequest{
		StoreRoot:    request.StoreDir,
		Repository:   candidate.Repository,
		PackageName:  previous.Package,
		Version:      candidate.LatestVersion,
		AssetDigest:  evidence.AssetDigest,
		ArtifactPath: artifactPath,
	})
	if err != nil {
		return result, err
	}

	extracted, err := u.archives.ExtractArchive(ctx, ArchiveExtractionRequest{
		ArchivePath:    layout.ArtifactPath,
		ArchiveName:    assetName,
		DestinationDir: layout.ExtractedDir,
		Binaries:       candidate.Package.Binaries,
	})
	if err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, err)
	}

	verificationRecord := VerificationRecord{
		SchemaVersion: 1,
		Repository:    candidate.Repository.String(),
		Package:       previous.Package,
		Version:       candidate.LatestVersion,
		Tag:           string(tag),
		Asset:         assetName,
		Evidence:      evidence,
	}
	evidencePath, err := u.evidence.WriteVerificationEvidence(ctx, layout.StorePath, verificationRecord)
	if err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, fmt.Errorf("write verification evidence: %w", err))
	}

	nextBinaries, err := plannedInstalledBinaries(request.BinDir, extracted)
	if err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, err)
	}

	installRecord := InstallRecord{
		SchemaVersion:    1,
		Repository:       candidate.Repository.String(),
		Package:          previous.Package,
		Version:          candidate.LatestVersion,
		Tag:              string(tag),
		Asset:            assetName,
		AssetDigest:      evidence.AssetDigest.String(),
		StorePath:        layout.StorePath,
		ArtifactPath:     layout.ArtifactPath,
		ExtractedPath:    layout.ExtractedDir,
		VerificationPath: evidencePath,
		Binaries:         nextBinaries,
	}
	if _, err := u.files.WriteInstallMetadata(ctx, layout.StorePath, installRecord); err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, fmt.Errorf("write install metadata: %w", err))
	}

	previousBinaries := installedBinaries(previous.Binaries)
	if err := u.files.ReplaceManagedBinaries(ctx, ReplaceManagedBinariesRequest{
		BinDir:   request.BinDir,
		Previous: previousBinaries,
		Next:     nextBinaries,
	}); err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, err)
	}

	current := state.Record{
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
		Binaries:         stateBinaries(nextBinaries),
		InstalledAt:      u.now().UTC(),
	}
	if _, err := u.state.ReplaceInstalledRecord(ctx, request.StateDir, current); err != nil {
		rollbackErr := u.files.ReplaceManagedBinaries(context.WithoutCancel(ctx), ReplaceManagedBinariesRequest{
			BinDir:   request.BinDir,
			Previous: nextBinaries,
			Next:     previousBinaries,
		})
		var cleanupErr error
		if rollbackErr == nil {
			cleanupErr = u.files.RemoveManagedInstall(context.WithoutCancel(ctx), RemoveManagedInstallRequest{
				StoreRoot: request.StoreDir,
				BinRoot:   request.BinDir,
				StorePath: layout.StorePath,
			})
		}
		var preservedErr error
		if rollbackErr != nil {
			preservedErr = fmt.Errorf("preserved staged update at %s after rollback failure", layout.StorePath)
		}
		return result, errors.Join(
			fmt.Errorf("replace installed state: %w", err),
			wrapOptional("restore previous managed binaries", rollbackErr),
			preservedErr,
			wrapOptional("cleanup staged update", cleanupErr),
		)
	}

	result = updateRecordResult{
		Previous: previous,
		Current:  current,
		Updated:  true,
	}
	if err := u.files.RemoveManagedStore(context.WithoutCancel(ctx), request.StoreDir, previous.StorePath); err != nil {
		return result, fmt.Errorf(
			"updated %s/%s@%s -> %s but failed to remove previous store: %w",
			previous.Repository,
			previous.Package,
			previous.Version,
			current.Version,
			err,
		)
	}
	return result, nil
}

func (u *PackageUpdater) checkBinaryOwnership(ctx context.Context, stateDir string, previous state.Record, binaries []manifest.Binary) error {
	index, err := u.state.LoadInstalledState(ctx, stateDir)
	if err != nil {
		return err
	}
	owner := state.PackageRef{Repository: previous.Repository, Package: previous.Package}
	return index.CheckBinaryOwnership(owner, manifestBinaryNames(binaries), owner)
}

func (r UpdateRequest) validate() error {
	if r.All && strings.TrimSpace(r.Target) != "" {
		return fmt.Errorf("update accepts a target or --all, not both")
	}
	if !r.All && strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("update target must be set")
	}
	if strings.TrimSpace(r.StoreDir) == "" {
		return fmt.Errorf("store directory must be set")
	}
	if strings.TrimSpace(r.BinDir) == "" {
		return fmt.Errorf("bin directory must be set")
	}
	if strings.TrimSpace(r.StateDir) == "" {
		return fmt.Errorf("state directory must be set")
	}
	return nil
}

func updateRow(result updateRecordResult, err error) UpdateInstalledResult {
	row := UpdateInstalledResult{
		Repository:      result.Previous.Repository,
		Package:         result.Previous.Package,
		PreviousVersion: result.Previous.Version,
		CurrentVersion:  result.Current.Version,
	}
	switch {
	case err == nil && result.Updated:
		row.Status = UpdateStatusUpdated
	case err == nil:
		row.Status = UpdateStatusAlreadyUpToDate
	case result.Updated:
		row.Status = UpdateStatusUpdatedWithWarning
		row.Reason = err.Error()
	default:
		row.Status = UpdateStatusCannotUpdate
		row.Reason = err.Error()
	}
	return row
}

func (u *PackageUpdater) cleanupStagedUpdate(ctx context.Context, request UpdateRequest, storePath string, err error) error {
	cleanupErr := u.files.RemoveManagedInstall(context.WithoutCancel(ctx), RemoveManagedInstallRequest{
		StoreRoot: request.StoreDir,
		BinRoot:   request.BinDir,
		StorePath: storePath,
	})
	if cleanupErr != nil {
		return errors.Join(err, fmt.Errorf("cleanup staged update: %w", cleanupErr))
	}
	return err
}

func plannedInstalledBinaries(binDir string, binaries []ExtractedBinary) ([]InstalledBinary, error) {
	binDir = strings.TrimSpace(binDir)
	if binDir == "" {
		return nil, fmt.Errorf("bin directory must be set")
	}
	binRoot, err := filepath.Abs(filepath.Clean(binDir))
	if err != nil {
		return nil, fmt.Errorf("resolve bin directory: %w", err)
	}
	if binRoot == string(os.PathSeparator) {
		return nil, fmt.Errorf("refusing to use unsafe bin directory %s", binDir)
	}
	if len(binaries) == 0 {
		return nil, fmt.Errorf("at least one binary must be linked")
	}
	planned := make([]InstalledBinary, 0, len(binaries))
	for _, binary := range binaries {
		name := strings.TrimSpace(binary.Name)
		if name == "" {
			return nil, fmt.Errorf("binary name must be set")
		}
		if strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("binary name %q must not contain path separators", binary.Name)
		}
		if strings.TrimSpace(binary.Path) == "" {
			return nil, fmt.Errorf("binary %q target path must be set", binary.Name)
		}
		planned = append(planned, InstalledBinary{
			Name:       name,
			LinkPath:   filepath.Join(binRoot, name),
			TargetPath: binary.Path,
		})
	}
	return planned, nil
}

func wrapOptional(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", label, err)
}
