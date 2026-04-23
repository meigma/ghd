package app

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

// VerificationRecordStore loads persisted verification evidence.
type VerificationRecordStore interface {
	// ReadVerificationRecord reads one persisted verification record.
	ReadVerificationRecord(ctx context.Context, path string) (VerificationRecord, error)
}

// InstalledVerificationFileSystem owns verify-time temporary directories and comparisons.
type InstalledVerificationFileSystem interface {
	// CreateDownloadDir creates a temporary directory suitable for short-lived verification work.
	CreateDownloadDir(ctx context.Context) (string, func(), error)
	// VerifyManagedBinaryLink verifies that linkPath is a symlink to expectedTargetPath.
	VerifyManagedBinaryLink(ctx context.Context, linkPath string, expectedTargetPath string) error
	// CompareFiles verifies that both files have identical contents and the live file remains executable.
	CompareFiles(ctx context.Context, path string, otherPath string) error
}

// InstalledPackageVerifierDependencies contains the ports needed by InstalledPackageVerifier.
type InstalledPackageVerifierDependencies struct {
	// StateStore loads active installed package records.
	StateStore InstalledStateReader
	// Verifier re-verifies stored release artifacts.
	Verifier Verifier
	// EvidenceStore loads persisted verification evidence.
	EvidenceStore VerificationRecordStore
	// Archives extracts verified archives for binary comparison.
	Archives ArchiveExtractor
	// FileSystem owns verify-time temporary directories and file comparisons.
	FileSystem InstalledVerificationFileSystem
}

// VerifyInstalledRequest describes one installed-package verification.
type VerifyInstalledRequest struct {
	// Target is a package name, binary name, or owner/repo/package target.
	Target string
	// StateDir stores active installed package state.
	StateDir string
}

// InstalledPackageVerifier re-verifies one installed package.
type InstalledPackageVerifier struct {
	state    InstalledStateReader
	verify   Verifier
	evidence VerificationRecordStore
	archives ArchiveExtractor
	files    InstalledVerificationFileSystem
}

// NewInstalledPackageVerifier creates an installed package verifier use case.
func NewInstalledPackageVerifier(deps InstalledPackageVerifierDependencies) (*InstalledPackageVerifier, error) {
	if deps.StateStore == nil {
		return nil, fmt.Errorf("installed state store must be set")
	}
	if deps.Verifier == nil {
		return nil, fmt.Errorf("verifier must be set")
	}
	if deps.EvidenceStore == nil {
		return nil, fmt.Errorf("verification record store must be set")
	}
	if deps.Archives == nil {
		return nil, fmt.Errorf("archive extractor must be set")
	}
	if deps.FileSystem == nil {
		return nil, fmt.Errorf("verify filesystem must be set")
	}
	return &InstalledPackageVerifier{
		state:    deps.StateStore,
		verify:   deps.Verifier,
		evidence: deps.EvidenceStore,
		archives: deps.Archives,
		files:    deps.FileSystem,
	}, nil
}

// Verify re-validates one active installed package and its managed binaries.
func (v *InstalledPackageVerifier) Verify(ctx context.Context, request VerifyInstalledRequest) (state.Record, error) {
	if err := request.validate(); err != nil {
		return state.Record{}, err
	}
	index, err := v.state.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return state.Record{}, err
	}
	record, err := index.ResolveTarget(request.Target)
	if err != nil {
		return state.Record{}, err
	}

	verificationRecord, err := v.evidence.ReadVerificationRecord(ctx, record.VerificationPath)
	if err != nil {
		return state.Record{}, err
	}
	if err := verifyInstalledRecordConsistency(record, verificationRecord); err != nil {
		return state.Record{}, err
	}
	signerWorkflow := verificationRecord.Evidence.ProvenanceAttestation.SignerWorkflow
	if strings.TrimSpace(string(signerWorkflow)) == "" {
		return state.Record{}, fmt.Errorf("verification evidence for %s/%s@%s has no trusted signer workflow", record.Repository, record.Package, record.Version)
	}

	repository, err := parseRecordRepository(record.Repository)
	if err != nil {
		return state.Record{}, err
	}
	evidence, err := v.verify.VerifyReleaseAsset(ctx, verification.Request{
		Repository: repository,
		Tag:        verification.ReleaseTag(record.Tag),
		AssetPath:  record.ArtifactPath,
		Policy: verification.Policy{
			TrustedSignerWorkflow: signerWorkflow,
		},
	})
	if err != nil {
		return state.Record{}, err
	}
	if evidence.AssetDigest.String() != record.AssetDigest {
		return state.Record{}, fmt.Errorf("re-verified artifact digest %s does not match installed digest %s", evidence.AssetDigest, record.AssetDigest)
	}
	if evidence.AssetDigest.String() != verificationRecord.Evidence.AssetDigest.String() {
		return state.Record{}, fmt.Errorf("re-verified artifact digest %s does not match persisted verification digest %s", evidence.AssetDigest, verificationRecord.Evidence.AssetDigest)
	}

	declaredBinaries, installedByRelativePath, err := installedBinaryDeclarations(record)
	if err != nil {
		return state.Record{}, err
	}
	tempDir, cleanup, err := v.files.CreateDownloadDir(ctx)
	if err != nil {
		return state.Record{}, err
	}
	defer cleanup()

	extracted, err := v.archives.ExtractArchive(ctx, ArchiveExtractionRequest{
		ArchivePath:    record.ArtifactPath,
		ArchiveName:    record.Asset,
		DestinationDir: tempDir,
		Binaries:       declaredBinaries,
	})
	if err != nil {
		return state.Record{}, err
	}
	extractedByRelativePath := map[string]ExtractedBinary{}
	for _, binary := range extracted {
		key := path.Clean(filepath.ToSlash(strings.TrimSpace(binary.RelativePath)))
		extractedByRelativePath[key] = binary
	}

	for relativePath, installedBinary := range installedByRelativePath {
		extractedBinary, ok := extractedByRelativePath[relativePath]
		if !ok {
			return state.Record{}, fmt.Errorf("verified artifact did not extract installed binary %q at %s", installedBinary.Name, relativePath)
		}
		if err := v.files.VerifyManagedBinaryLink(ctx, installedBinary.LinkPath, installedBinary.TargetPath); err != nil {
			return state.Record{}, err
		}
		if err := v.files.CompareFiles(ctx, installedBinary.TargetPath, extractedBinary.Path); err != nil {
			return state.Record{}, fmt.Errorf("installed binary %q does not match verified artifact: %w", installedBinary.Name, err)
		}
	}

	return record, nil
}

func (r VerifyInstalledRequest) validate() error {
	if strings.TrimSpace(r.Target) == "" {
		return fmt.Errorf("verify target must be set")
	}
	if strings.TrimSpace(r.StateDir) == "" {
		return fmt.Errorf("state directory must be set")
	}
	return nil
}

func verifyInstalledRecordConsistency(record state.Record, verificationRecord VerificationRecord) error {
	fields := []struct {
		label     string
		installed string
		recorded  string
	}{
		{label: "repository", installed: record.Repository, recorded: verificationRecord.Repository},
		{label: "package", installed: record.Package, recorded: verificationRecord.Package},
		{label: "version", installed: record.Version, recorded: verificationRecord.Version},
		{label: "tag", installed: record.Tag, recorded: verificationRecord.Tag},
		{label: "asset", installed: record.Asset, recorded: verificationRecord.Asset},
	}
	for _, field := range fields {
		if field.installed != field.recorded {
			return fmt.Errorf("installed %s %q does not match persisted verification %s %q", field.label, field.installed, field.label, field.recorded)
		}
	}
	return nil
}

func installedBinaryDeclarations(record state.Record) ([]manifest.Binary, map[string]state.Binary, error) {
	if strings.TrimSpace(record.ExtractedPath) == "" {
		return nil, nil, fmt.Errorf("installed extracted path must be set")
	}
	declared := make([]manifest.Binary, 0, len(record.Binaries))
	byRelativePath := make(map[string]state.Binary, len(record.Binaries))
	for _, binary := range record.Binaries {
		relativePath, err := filepath.Rel(record.ExtractedPath, binary.TargetPath)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve installed binary %q relative path: %w", binary.Name, err)
		}
		normalized := path.Clean(filepath.ToSlash(relativePath))
		if normalized == "." || normalized == ".." || path.IsAbs(normalized) || strings.HasPrefix(normalized, "../") {
			return nil, nil, fmt.Errorf("installed binary %q target %s escapes extracted path %s", binary.Name, binary.TargetPath, record.ExtractedPath)
		}
		decl := manifest.Binary{Path: normalized}
		if err := decl.Validate(); err != nil {
			return nil, nil, fmt.Errorf("installed binary %q path %s is invalid: %w", binary.Name, normalized, err)
		}
		if _, ok := byRelativePath[normalized]; ok {
			return nil, nil, fmt.Errorf("installed binary path %s is declared more than once", normalized)
		}
		declared = append(declared, decl)
		byRelativePath[normalized] = binary
	}
	return declared, byRelativePath, nil
}
