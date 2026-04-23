package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestInstalledPackageVerifierVerify(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)

		record, err := tc.subject.Verify(context.Background(), tc.request)

		require.NoError(t, err)
		assert.Equal(t, tc.record.Repository, record.Repository)
		assert.Equal(t, tc.record.Package, record.Package)
	})

	t.Run("missing artifact", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.ArtifactPath))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing artifact")
	})

	t.Run("missing verification record", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.VerificationPath))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read verification record")
	})

	t.Run("malformed verification record", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.WriteFile(tc.record.VerificationPath, []byte("{\n"), 0o600))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode verification record")
	})

	t.Run("artifact digest mismatch", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		tc.verifier.evidence.AssetDigest = mustTestDigest(t, "bb")

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "re-verified artifact digest")
	})

	t.Run("broken link", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.Binaries[0].LinkPath))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "inspect managed binary link")
	})

	t.Run("non-symlink link", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.Binaries[0].LinkPath))
		require.NoError(t, os.WriteFile(tc.record.Binaries[0].LinkPath, []byte("not a symlink"), 0o644))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a symlink")
	})

	t.Run("target outside extracted path", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		outsideDir := filepath.Join(t.TempDir(), "outside")
		require.NoError(t, os.MkdirAll(outsideDir, 0o755))
		outsidePath := filepath.Join(outsideDir, "foo")
		require.NoError(t, os.WriteFile(outsidePath, []byte("binary"), 0o755))
		tc.record.Binaries[0].TargetPath = outsidePath
		tc.state.index = mustStateIndex(t, tc.record)

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes extracted path")
	})

	t.Run("binary drift", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.WriteFile(tc.record.Binaries[0].TargetPath, []byte("tampered"), 0o755))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match verified artifact")
	})

	t.Run("executable bit drift", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Chmod(tc.record.Binaries[0].TargetPath, 0o644))

		_, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not executable")
	})
}

type installedPackageVerifierTestContext struct {
	subject  *InstalledPackageVerifier
	state    *fakeInstalledStateReader
	verifier *fakeInstalledArtifactVerifier
	record   state.Record
	request  VerifyInstalledRequest
}

func newInstalledPackageVerifierTestContext(t *testing.T) *installedPackageVerifierTestContext {
	t.Helper()

	repository := verification.Repository{Owner: "owner", Name: "repo"}
	storePath := filepath.Join(t.TempDir(), "store")
	extractedPath := filepath.Join(storePath, "extracted")
	require.NoError(t, os.MkdirAll(extractedPath, 0o755))

	targetPath := filepath.Join(extractedPath, "foo")
	require.NoError(t, os.WriteFile(targetPath, []byte("binary"), 0o755))

	binDir := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	linkPath := filepath.Join(binDir, "foo")
	require.NoError(t, os.Symlink(targetPath, linkPath))

	artifactPath := filepath.Join(storePath, "artifact.tar.gz")
	require.NoError(t, os.WriteFile(artifactPath, []byte("artifact"), 0o600))

	digest := mustTestDigest(t, "aa")
	evidenceStore := fakeVerificationRecordStore{}
	verificationPath, err := evidenceStore.WriteVerificationRecord(context.Background(), storePath, VerificationRecord{
		SchemaVersion: 1,
		Repository:    repository.String(),
		Package:       "foo",
		Version:       "1.2.3",
		Tag:           "v1.2.3",
		Asset:         "foo.tar.gz",
		Evidence: verification.Evidence{
			Repository:  repository,
			Tag:         "v1.2.3",
			AssetDigest: digest,
			ProvenanceAttestation: verification.AttestationEvidence{
				SignerWorkflow: verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"),
			},
		},
	})
	require.NoError(t, err)

	record := state.Record{
		Repository:       repository.String(),
		Package:          "foo",
		Version:          "1.2.3",
		Tag:              "v1.2.3",
		Asset:            "foo.tar.gz",
		AssetDigest:      digest.String(),
		StorePath:        storePath,
		ArtifactPath:     artifactPath,
		ExtractedPath:    extractedPath,
		VerificationPath: verificationPath,
		Binaries: []state.Binary{
			{Name: "foo", LinkPath: linkPath, TargetPath: targetPath},
		},
		InstalledAt: time.Unix(1700000000, 0).UTC(),
	}

	stateReader := &fakeInstalledStateReader{index: mustStateIndex(t, record)}
	artifactVerifier := &fakeInstalledArtifactVerifier{
		evidence: verification.Evidence{
			Repository:  repository,
			Tag:         "v1.2.3",
			AssetDigest: digest,
			ProvenanceAttestation: verification.AttestationEvidence{
				SignerWorkflow: verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"),
			},
		},
	}
	subject, err := NewInstalledPackageVerifier(InstalledPackageVerifierDependencies{
		StateStore:    stateReader,
		Verifier:      artifactVerifier,
		EvidenceStore: evidenceStore,
		Archives:      fakeVerifyArchiveExtractor{contents: map[string][]byte{"foo": []byte("binary")}},
		FileSystem:    fakeInstalledVerificationFileSystem{},
	})
	require.NoError(t, err)

	return &installedPackageVerifierTestContext{
		subject:  subject,
		state:    stateReader,
		verifier: artifactVerifier,
		record:   record,
		request: VerifyInstalledRequest{
			Target:   "foo",
			StateDir: filepath.Join(t.TempDir(), "state"),
		},
	}
}

type fakeInstalledStateReader struct {
	index state.Index
	err   error
}

func (f *fakeInstalledStateReader) LoadInstalledState(context.Context, string) (state.Index, error) {
	if f.err != nil {
		return state.Index{}, f.err
	}
	return f.index, nil
}

type fakeInstalledArtifactVerifier struct {
	evidence verification.Evidence
}

func (f *fakeInstalledArtifactVerifier) VerifyReleaseAsset(_ context.Context, request verification.Request) (verification.Evidence, error) {
	if _, err := os.Stat(request.AssetPath); err != nil {
		return verification.Evidence{}, fmt.Errorf("missing artifact: %w", err)
	}
	evidence := f.evidence
	evidence.Repository = request.Repository
	evidence.Tag = request.Tag
	evidence.ProvenanceAttestation.SignerWorkflow = request.Policy.TrustedSignerWorkflow
	return evidence, nil
}

type fakeVerificationRecordStore struct{}

func (fakeVerificationRecordStore) ReadVerificationRecord(_ context.Context, path string) (VerificationRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return VerificationRecord{}, fmt.Errorf("read verification record: %w", err)
	}
	var record VerificationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return VerificationRecord{}, fmt.Errorf("decode verification record: %w", err)
	}
	if err := record.Validate(); err != nil {
		return VerificationRecord{}, err
	}
	return record, nil
}

func (fakeVerificationRecordStore) WriteVerificationRecord(_ context.Context, outputDir string, record VerificationRecord) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	path := filepath.Join(outputDir, "verification.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

type fakeVerifyArchiveExtractor struct {
	contents map[string][]byte
}

func (f fakeVerifyArchiveExtractor) ExtractArchive(_ context.Context, request ArchiveExtractionRequest) ([]ExtractedBinary, error) {
	if err := os.MkdirAll(request.DestinationDir, 0o755); err != nil {
		return nil, err
	}
	out := make([]ExtractedBinary, 0, len(request.Binaries))
	for _, binary := range request.Binaries {
		targetPath := filepath.Join(request.DestinationDir, filepath.FromSlash(binary.Path))
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, err
		}
		content := f.contents[binary.Path]
		if len(content) == 0 {
			content = []byte("binary")
		}
		if err := os.WriteFile(targetPath, content, 0o755); err != nil {
			return nil, err
		}
		out = append(out, ExtractedBinary{
			Name:         filepath.Base(binary.Path),
			RelativePath: binary.Path,
			Path:         targetPath,
		})
	}
	return out, nil
}

type fakeInstalledVerificationFileSystem struct{}

func (fakeInstalledVerificationFileSystem) CreateDownloadDir(context.Context) (string, func(), error) {
	dir, err := os.MkdirTemp("", "ghd-verify-*")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func (fakeInstalledVerificationFileSystem) VerifyManagedBinaryLink(_ context.Context, linkPath string, expectedTargetPath string) error {
	info, err := os.Lstat(linkPath)
	if err != nil {
		return fmt.Errorf("inspect managed binary link %s: %w", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("managed binary link %s is not a symlink", linkPath)
	}
	targetPath, err := os.Readlink(linkPath)
	if err != nil {
		return fmt.Errorf("read managed binary link %s: %w", linkPath, err)
	}
	if targetPath != expectedTargetPath {
		return fmt.Errorf("managed binary link %s points to %s, not %s", linkPath, targetPath, expectedTargetPath)
	}
	return nil
}

func (fakeInstalledVerificationFileSystem) CompareFiles(_ context.Context, path string, otherPath string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("file %s is not executable", path)
	}
	left, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}
	right, err := os.ReadFile(otherPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", otherPath, err)
	}
	if string(left) != string(right) {
		return fmt.Errorf("file %s does not match %s", path, otherPath)
	}
	return nil
}

func mustStateIndex(t *testing.T, record state.Record) state.Index {
	t.Helper()
	index, err := state.NewIndex().AddRecord(record)
	require.NoError(t, err)
	return index
}

func mustTestDigest(t *testing.T, pair string) verification.Digest {
	t.Helper()
	digest, err := verification.NewDigest("sha256", strings.Repeat(pair, 32))
	require.NoError(t, err)
	return digest
}
