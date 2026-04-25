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
	// EvidenceStore loads persisted verification evidence for the installed version.
	EvidenceStore VerificationRecordStore
	// Materializer prepares configured binaries from verified artifacts.
	Materializer ArtifactMaterializer
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

// ErrUpdateNotApproved means update stopped because the verified artifact was not approved.
var ErrUpdateNotApproved = errors.New("update was not approved")

// ErrUpdateSignerChangeNotApproved means update stopped because it would rotate the trusted release signer.
var ErrUpdateSignerChangeNotApproved = errors.New("update would change the trusted release signer; review interactively or rerun with --yes --approve-signer-change --non-interactive")

// UpdateProgressStage identifies one user-visible update step.
type UpdateProgressStage string

const (
	// UpdateProgressCheckingState means update is reading active installed package state.
	UpdateProgressCheckingState UpdateProgressStage = "checking-state"
	// UpdateProgressResolvingCandidate means update is checking package metadata and releases.
	UpdateProgressResolvingCandidate UpdateProgressStage = "resolving-candidate"
	// UpdateProgressCheckingBinaries means update is checking future binary ownership.
	UpdateProgressCheckingBinaries UpdateProgressStage = "checking-binaries"
	// UpdateProgressResolvingAsset means update is resolving the concrete GitHub release asset.
	UpdateProgressResolvingAsset UpdateProgressStage = "resolving-asset"
	// UpdateProgressPreparingDownload means update is preparing temporary download storage.
	UpdateProgressPreparingDownload UpdateProgressStage = "preparing-download"
	// UpdateProgressDownloading means update is downloading the selected release asset.
	UpdateProgressDownloading UpdateProgressStage = "downloading"
	// UpdateProgressVerifying means update is verifying release and provenance attestations.
	UpdateProgressVerifying UpdateProgressStage = "verifying"
	// UpdateProgressAwaitingApproval means update has verified the asset and is waiting for approval.
	UpdateProgressAwaitingApproval UpdateProgressStage = "awaiting-approval"
	// UpdateProgressPreparingStore means update is creating the managed store layout.
	UpdateProgressPreparingStore UpdateProgressStage = "preparing-store"
	// UpdateProgressExtracting means update is preparing configured binaries.
	UpdateProgressExtracting UpdateProgressStage = "extracting"
	// UpdateProgressWritingEvidence means update is writing verification evidence.
	UpdateProgressWritingEvidence UpdateProgressStage = "writing-evidence"
	// UpdateProgressWritingMetadata means update is writing package install metadata.
	UpdateProgressWritingMetadata UpdateProgressStage = "writing-metadata"
	// UpdateProgressReplacingBinaries means update is swapping managed binary links.
	UpdateProgressReplacingBinaries UpdateProgressStage = "replacing-binaries"
	// UpdateProgressRecordingState means update is recording active installed package state.
	UpdateProgressRecordingState UpdateProgressStage = "recording-state"
	// UpdateProgressRemovingPreviousStore means update is cleaning up the previous managed store.
	UpdateProgressRemovingPreviousStore UpdateProgressStage = "removing-previous-store"
)

// UpdateProgress describes one user-visible update step.
type UpdateProgress struct {
	// Stage identifies the step underway.
	Stage UpdateProgressStage
	// Message is a concise human-readable status line.
	Message string
	// Download carries byte-level download progress when Stage is UpdateProgressDownloading.
	Download *DownloadProgress
}

// UpdateProgressFunc receives user-visible update progress.
type UpdateProgressFunc func(UpdateProgress)

// UpdateApproval contains verified artifact facts for the update approval decision.
type UpdateApproval struct {
	// Repository is the verified GitHub repository.
	Repository verification.Repository
	// PackageName is the package name within the repository manifest.
	PackageName manifest.PackageName
	// PreviousVersion is the active version that will be replaced.
	PreviousVersion manifest.PackageVersion
	// Version is the candidate version that will become active.
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
	// TrustedSignerWorkflow is the signer previously trusted by the installed package.
	TrustedSignerWorkflow verification.WorkflowIdentity
	// CandidateSignerWorkflow is the signer declared by the candidate release and accepted during verification.
	CandidateSignerWorkflow verification.WorkflowIdentity
	// SignerChanged reports whether the candidate signer differs from the previously trusted signer.
	SignerChanged bool
	// TrustRootPath is the custom Sigstore trusted_root.json path, when configured.
	TrustRootPath string
	// BinDir receives exposed binary links if the update proceeds.
	BinDir string
	// Binaries are the binary names that will be exposed if the update proceeds.
	Binaries []string
}

// UpdateApprovalFunc approves or rejects a verified update before local mutation.
type UpdateApprovalFunc func(context.Context, UpdateApproval) error

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
	manifests    ManifestSource
	releases     RepositoryReleaseSource
	assets       ReleaseAssetSource
	download     ArtifactDownloader
	verify       Verifier
	evidence     EvidenceWriter
	records      VerificationRecordStore
	materializer ArtifactMaterializer
	files        UpdateFileSystem
	state        InstalledStateReplaceStore
	now          func() time.Time
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
	// TrustRootPath is the custom Sigstore trusted_root.json path, when configured.
	TrustRootPath string
	// AllowSignerChange approves signer rotation when no interactive callback is present.
	AllowSignerChange bool
	// Progress receives user-visible update progress. Nil disables progress reports.
	Progress UpdateProgressFunc
	// Approve approves a verified artifact before preparing binaries, linking, or state writes. Nil approves automatically.
	Approve UpdateApprovalFunc
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
	if deps.EvidenceStore == nil {
		return nil, fmt.Errorf("verification record store must be set")
	}
	if deps.Materializer == nil {
		return nil, fmt.Errorf("artifact materializer must be set")
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
		manifests:    deps.Manifests,
		releases:     deps.Releases,
		assets:       deps.Assets,
		download:     deps.Downloader,
		verify:       deps.Verifier,
		evidence:     deps.EvidenceWriter,
		records:      deps.EvidenceStore,
		materializer: deps.Materializer,
		files:        deps.FileSystem,
		state:        deps.StateStore,
		now:          now,
	}, nil
}

// Update updates selected active installed packages.
func (u *PackageUpdater) Update(ctx context.Context, request UpdateRequest) ([]UpdateInstalledResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	request.report(UpdateProgressCheckingState, "Checking installed packages")
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

	target := previous.Repository + "/" + previous.Package
	request.report(UpdateProgressResolvingCandidate, fmt.Sprintf("Checking %s for updates", target))
	candidate, err := resolveInstalledPackageUpdate(ctx, u.manifests, u.releases, previous)
	if err != nil {
		return result, err
	}
	if candidate.LatestVersion.IsZero() {
		return result, nil
	}
	packageName, err := manifest.NewPackageName(previous.Package)
	if err != nil {
		return result, err
	}
	previousVersion, err := manifest.NewPackageVersion(previous.Version)
	if err != nil {
		return result, err
	}
	previousVerification, err := u.records.ReadVerificationRecord(ctx, previous.VerificationPath)
	if err != nil {
		return result, err
	}
	if err := verifyInstalledRecordConsistency(previous, previousVerification); err != nil {
		return result, err
	}
	trustedSignerWorkflow := previousVerification.Evidence.ProvenanceAttestation.SignerWorkflow
	if strings.TrimSpace(string(trustedSignerWorkflow)) == "" {
		return result, fmt.Errorf("verification evidence for %s/%s@%s has no trusted signer workflow", previous.Repository, previous.Package, previous.Version)
	}
	candidateSignerWorkflow := candidate.Config.Provenance.TrustedSignerWorkflow()
	if strings.TrimSpace(string(candidateSignerWorkflow)) == "" {
		return result, fmt.Errorf("candidate manifest for %s/%s@%s has no trusted signer workflow", previous.Repository, previous.Package, candidate.LatestVersion)
	}
	signerChanged := !trustedSignerWorkflow.SameWorkflowPath(candidateSignerWorkflow)
	request.report(UpdateProgressCheckingBinaries, fmt.Sprintf("Checking %s binary ownership", target))
	if err := u.checkBinaryOwnership(ctx, request.StateDir, previous, candidate.Package.Binaries); err != nil {
		return result, err
	}

	tag := candidate.Tag
	assetName, err := candidate.CandidateAsset.ResolveName(candidate.LatestVersion)
	if err != nil {
		return result, err
	}
	request.report(UpdateProgressResolvingAsset, fmt.Sprintf("Resolving %s", assetName))
	releaseAsset, err := u.assets.ResolveReleaseAsset(ctx, candidate.Repository, tag, assetName)
	if err != nil {
		return result, fmt.Errorf("resolve release asset %q: %w", assetName, err)
	}

	request.report(UpdateProgressPreparingDownload, "Preparing download")
	downloadDir, cleanup, err := u.files.CreateDownloadDir(ctx)
	if err != nil {
		return result, err
	}
	defer cleanup()

	request.report(UpdateProgressDownloading, fmt.Sprintf("Downloading %s", releaseAsset.Name))
	downloadRequest := DownloadReleaseAssetRequest{
		Asset:     releaseAsset,
		OutputDir: downloadDir,
	}
	if request.Progress != nil {
		downloadRequest.Progress = func(progress DownloadProgress) {
			request.reportDownload(progress)
		}
	}
	artifactPath, err := u.download.DownloadReleaseAsset(ctx, downloadRequest)
	if err != nil {
		return result, fmt.Errorf("download release asset %q: %w", releaseAsset.Name, err)
	}
	request.report(UpdateProgressVerifying, "Verifying release and provenance")
	evidence, err := u.verify.VerifyReleaseAsset(ctx, verification.Request{
		Repository: candidate.Repository,
		Tag:        tag,
		AssetPath:  artifactPath,
		Policy: verification.Policy{
			TrustedSignerWorkflow: candidateSignerWorkflow,
		},
	})
	if err != nil {
		return result, err
	}

	request.report(UpdateProgressAwaitingApproval, fmt.Sprintf("Reviewing verified update for %s", target))
	if err := request.approve(ctx, UpdateApproval{
		Repository:              candidate.Repository,
		PackageName:             packageName,
		PreviousVersion:         previousVersion,
		Version:                 candidate.LatestVersion,
		Tag:                     tag,
		AssetName:               assetName,
		AssetDigest:             evidence.AssetDigest,
		ReleasePredicateType:    evidence.ReleaseAttestation.PredicateType,
		ProvenancePredicateType: evidence.ProvenanceAttestation.PredicateType,
		TrustedSignerWorkflow:   trustedSignerWorkflow,
		CandidateSignerWorkflow: candidateSignerWorkflow,
		SignerChanged:           signerChanged,
		TrustRootPath:           request.TrustRootPath,
		BinDir:                  request.BinDir,
		Binaries:                manifestBinaryNames(candidate.Package.Binaries),
	}); err != nil {
		return result, err
	}

	request.report(UpdateProgressPreparingStore, "Preparing managed store")
	layout, err := u.files.CreateStoreLayout(ctx, StoreLayoutRequest{
		StoreRoot:    request.StoreDir,
		Repository:   candidate.Repository,
		PackageName:  packageName,
		Version:      candidate.LatestVersion,
		AssetDigest:  evidence.AssetDigest,
		ArtifactPath: artifactPath,
	})
	if err != nil {
		return result, err
	}

	request.report(UpdateProgressExtracting, "Preparing configured binaries")
	materialized, err := u.materializer.MaterializeBinaries(ctx, ArtifactMaterializationRequest{
		ArtifactPath:   layout.ArtifactPath,
		AssetName:      assetName,
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
		Version:       candidate.LatestVersion.String(),
		Tag:           string(tag),
		Asset:         assetName,
		Evidence:      evidence,
	}
	request.report(UpdateProgressWritingEvidence, "Writing verification evidence")
	evidencePath, err := u.evidence.WriteVerificationEvidence(ctx, layout.StorePath, verificationRecord)
	if err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, fmt.Errorf("write verification evidence: %w", err))
	}

	nextBinaries, err := plannedInstalledBinaries(request.BinDir, materialized)
	if err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, err)
	}

	installRecord := InstallRecord{
		SchemaVersion:    1,
		Repository:       candidate.Repository.String(),
		Package:          previous.Package,
		Version:          candidate.LatestVersion.String(),
		Tag:              string(tag),
		Asset:            assetName,
		AssetDigest:      evidence.AssetDigest.String(),
		StorePath:        layout.StorePath,
		ArtifactPath:     layout.ArtifactPath,
		ExtractedPath:    layout.ExtractedDir,
		VerificationPath: evidencePath,
		Binaries:         nextBinaries,
	}
	request.report(UpdateProgressWritingMetadata, "Writing install metadata")
	if _, err := u.files.WriteInstallMetadata(ctx, layout.StorePath, installRecord); err != nil {
		return result, u.cleanupStagedUpdate(ctx, request, layout.StorePath, fmt.Errorf("write install metadata: %w", err))
	}

	previousBinaries := installedBinaries(previous.Binaries)
	request.report(UpdateProgressReplacingBinaries, "Swapping managed binaries")
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
	request.report(UpdateProgressRecordingState, "Recording installed state")
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
	request.report(UpdateProgressRemovingPreviousStore, "Removing previous store")
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

func (r UpdateRequest) report(stage UpdateProgressStage, message string) {
	if r.Progress == nil {
		return
	}
	r.Progress(UpdateProgress{
		Stage:   stage,
		Message: message,
	})
}

func (r UpdateRequest) reportDownload(progress DownloadProgress) {
	if r.Progress == nil {
		return
	}
	copied := progress
	message := "Downloading"
	if strings.TrimSpace(copied.AssetName) != "" {
		message = fmt.Sprintf("Downloading %s", copied.AssetName)
	}
	r.Progress(UpdateProgress{
		Stage:    UpdateProgressDownloading,
		Message:  message,
		Download: &copied,
	})
}

func (r UpdateRequest) approve(ctx context.Context, approval UpdateApproval) error {
	if approval.SignerChanged && r.Approve == nil {
		if r.AllowSignerChange {
			return nil
		}
		return ErrUpdateSignerChangeNotApproved
	}
	if r.Approve == nil {
		return nil
	}
	return r.Approve(ctx, approval)
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

func plannedInstalledBinaries(binDir string, binaries []MaterializedBinary) ([]InstalledBinary, error) {
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
