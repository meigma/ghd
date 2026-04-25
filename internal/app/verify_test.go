package app

import (
	"context"
	"encoding/json"
	"errors"
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

		results, err := tc.subject.Verify(context.Background(), tc.request)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, VerifyInstalledResult{
			Repository: tc.record.Repository,
			Package:    tc.record.Package,
			Version:    tc.record.Version,
			Status:     VerifyStatusVerified,
		}, results[0])
	})

	t.Run("success direct binary asset", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		tc.record.Asset = "foo_1.2.3_darwin_arm64"
		verificationPath, err := fakeVerificationRecordStore{}.WriteVerificationRecord(context.Background(), filepath.Dir(tc.record.VerificationPath), VerificationRecord{
			SchemaVersion: 1,
			Repository:    tc.record.Repository,
			Package:       tc.record.Package,
			Version:       tc.record.Version,
			Tag:           tc.record.Tag,
			Asset:         tc.record.Asset,
			Evidence: verification.Evidence{
				Repository:  verification.Repository{Owner: "owner", Name: "repo"},
				Tag:         verification.ReleaseTag(tc.record.Tag),
				AssetDigest: mustTestDigest(t, "aa"),
				ProvenanceAttestation: verification.AttestationEvidence{
					SignerWorkflow: verification.WorkflowIdentity(tc.record.Repository + "/.github/workflows/release.yml"),
				},
			},
		})
		require.NoError(t, err)
		tc.record.VerificationPath = verificationPath
		tc.state.index = mustStateIndex(t, tc.record)

		results, err := tc.subject.Verify(context.Background(), tc.request)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, VerifyStatusVerified, results[0].Status)
	})

	t.Run("missing artifact", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.ArtifactPath))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "missing artifact")
	})

	t.Run("missing verification record", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.VerificationPath))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "read verification record")
	})

	t.Run("malformed verification record", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.WriteFile(tc.record.VerificationPath, []byte("{\n"), 0o600))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "decode verification record")
	})

	t.Run("invalid persisted release tag", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		tc.record.Tag = ".bad"
		verificationPath, err := fakeVerificationRecordStore{}.WriteVerificationRecord(context.Background(), filepath.Dir(tc.record.VerificationPath), VerificationRecord{
			SchemaVersion: 1,
			Repository:    tc.record.Repository,
			Package:       tc.record.Package,
			Version:       tc.record.Version,
			Tag:           tc.record.Tag,
			Asset:         tc.record.Asset,
			Evidence: verification.Evidence{
				Repository:  verification.Repository{Owner: "owner", Name: "repo"},
				Tag:         verification.ReleaseTag(tc.record.Tag),
				AssetDigest: mustTestDigest(t, "aa"),
			},
		})
		require.NoError(t, err)
		tc.record.VerificationPath = verificationPath
		tc.state.index = mustStateIndex(t, tc.record)

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "verification tag")
	})

	t.Run("artifact digest mismatch", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		tc.verifier.evidence.AssetDigest = mustTestDigest(t, "bb")

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "re-verified artifact digest")
	})

	t.Run("broken link", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.Binaries[0].LinkPath))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "inspect managed binary link")
	})

	t.Run("non-symlink link", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Remove(tc.record.Binaries[0].LinkPath))
		require.NoError(t, os.WriteFile(tc.record.Binaries[0].LinkPath, []byte("not a symlink"), 0o644))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "is not a symlink")
	})

	t.Run("target outside extracted path", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		outsideDir := filepath.Join(t.TempDir(), "outside")
		require.NoError(t, os.MkdirAll(outsideDir, 0o755))
		outsidePath := filepath.Join(outsideDir, "foo")
		require.NoError(t, os.WriteFile(outsidePath, []byte("binary"), 0o755))
		tc.record.Binaries[0].TargetPath = outsidePath
		tc.state.index = mustStateIndex(t, tc.record)

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "escapes extracted path")
	})

	t.Run("binary drift", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.WriteFile(tc.record.Binaries[0].TargetPath, []byte("tampered"), 0o755))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "does not match verified artifact")
	})

	t.Run("executable bit drift", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		require.NoError(t, os.Chmod(tc.record.Binaries[0].TargetPath, 0o644))

		results, err := tc.subject.Verify(context.Background(), tc.request)

		assertSingleVerifyFailure(t, results, err, "is not executable")
	})
}

func TestInstalledPackageVerifierVerifyAll(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		second := newInstalledPackageVerifierRecordFixture(t, verification.Repository{Owner: "owner", Name: "alpha"}, "bar", "1.0.0")
		tc.state.index = mustStateIndex(t, tc.record, second)

		results, err := tc.subject.Verify(context.Background(), VerifyInstalledRequest{
			All:      true,
			StateDir: tc.request.StateDir,
		})

		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, VerifyInstalledResult{
			Repository: "owner/alpha",
			Package:    "bar",
			Version:    "1.0.0",
			Status:     VerifyStatusVerified,
		}, results[0])
		assert.Equal(t, VerifyInstalledResult{
			Repository: "owner/repo",
			Package:    "foo",
			Version:    "1.2.3",
			Status:     VerifyStatusVerified,
		}, results[1])
	})

	t.Run("mixed failure", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		second := newInstalledPackageVerifierRecordFixture(t, verification.Repository{Owner: "owner", Name: "alpha"}, "bar", "1.0.0")
		tc.state.index = mustStateIndex(t, tc.record, second)
		require.NoError(t, os.WriteFile(tc.record.Binaries[0].TargetPath, []byte("tampered"), 0o755))

		results, err := tc.subject.Verify(context.Background(), VerifyInstalledRequest{
			All:      true,
			StateDir: tc.request.StateDir,
		})

		require.Error(t, err)
		var incomplete VerifyIncompleteError
		require.ErrorAs(t, err, &incomplete)
		assert.Equal(t, 1, incomplete.Failed)
		require.Len(t, results, 2)
		assert.Equal(t, VerifyStatusVerified, results[0].Status)
		assert.Equal(t, VerifyStatusCannotVerify, results[1].Status)
		assert.Contains(t, results[1].Reason, "does not match verified artifact")
	})
}

func TestInstalledPackageVerifierVerifyRejectsInvalidRequests(t *testing.T) {
	tc := newInstalledPackageVerifierTestContext(t)

	results, err := tc.subject.Verify(context.Background(), VerifyInstalledRequest{
		StateDir: tc.request.StateDir,
	})

	require.Error(t, err)
	assert.Nil(t, results)
	assert.EqualError(t, err, "verify target must be set")
}

func TestInstalledPackageVerifierVerifyRejectsTargetAndAll(t *testing.T) {
	tc := newInstalledPackageVerifierTestContext(t)

	results, err := tc.subject.Verify(context.Background(), VerifyInstalledRequest{
		Target:   "foo",
		All:      true,
		StateDir: tc.request.StateDir,
	})

	require.Error(t, err)
	assert.Nil(t, results)
	assert.EqualError(t, err, "verify accepts a target or --all, not both")
}

func TestInstalledPackageVerifierVerifyReturnsPreflightErrorsWithoutResults(t *testing.T) {
	t.Run("state load failure", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		tc.state.err = errors.New("boom")

		results, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Nil(t, results)
		assert.EqualError(t, err, "boom")
	})

	t.Run("ambiguous target", func(t *testing.T) {
		tc := newInstalledPackageVerifierTestContext(t)
		tc.record.Binaries[0].Name = "repo"
		second := newInstalledPackageVerifierRecordFixture(t, verification.Repository{Owner: "owner", Name: "two"}, "bar", "1.0.0")
		second.Binaries[0].Name = "foo"
		tc.state.index = mustStateIndex(t, tc.record, second)

		results, err := tc.subject.Verify(context.Background(), tc.request)

		require.Error(t, err)
		assert.Nil(t, results)
		var ambiguous state.AmbiguousInstallError
		require.ErrorAs(t, err, &ambiguous)
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

	record := newInstalledPackageVerifierRecordFixture(t, verification.Repository{Owner: "owner", Name: "repo"}, "foo", "1.2.3")
	stateReader := &fakeInstalledStateReader{index: mustStateIndex(t, record)}
	artifactVerifier := &fakeInstalledArtifactVerifier{
		evidence: verification.Evidence{
			Repository:  verification.Repository{Owner: "owner", Name: "repo"},
			Tag:         "v1.2.3",
			AssetDigest: mustTestDigest(t, "aa"),
			ProvenanceAttestation: verification.AttestationEvidence{
				SignerWorkflow: verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"),
			},
		},
	}
	subject, err := NewInstalledPackageVerifier(InstalledPackageVerifierDependencies{
		StateStore:    stateReader,
		Verifier:      artifactVerifier,
		EvidenceStore: fakeVerificationRecordStore{},
		Materializer:  fakeVerifyArchiveExtractor{contents: map[string][]byte{"foo": []byte("binary")}},
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

func newInstalledPackageVerifierRecordFixture(t *testing.T, repository verification.Repository, packageName string, version string) state.Record {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "store")
	extractedPath := filepath.Join(storePath, "extracted")
	require.NoError(t, os.MkdirAll(extractedPath, 0o755))

	targetPath := filepath.Join(extractedPath, packageName)
	require.NoError(t, os.WriteFile(targetPath, []byte("binary"), 0o755))

	binDir := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	linkPath := filepath.Join(binDir, packageName)
	require.NoError(t, os.Symlink(targetPath, linkPath))

	assetName := packageName + ".tar.gz"
	artifactPath := filepath.Join(storePath, assetName)
	require.NoError(t, os.WriteFile(artifactPath, []byte("artifact"), 0o600))

	digest := mustTestDigest(t, "aa")
	evidenceStore := fakeVerificationRecordStore{}
	verificationPath, err := evidenceStore.WriteVerificationRecord(context.Background(), storePath, VerificationRecord{
		SchemaVersion: 1,
		Repository:    repository.String(),
		Package:       packageName,
		Version:       version,
		Tag:           "v" + version,
		Asset:         assetName,
		Evidence: verification.Evidence{
			Repository:  repository,
			Tag:         verification.ReleaseTag("v" + version),
			AssetDigest: digest,
			ProvenanceAttestation: verification.AttestationEvidence{
				SignerWorkflow: verification.WorkflowIdentity(repository.String() + "/.github/workflows/release.yml"),
			},
		},
	})
	require.NoError(t, err)

	return state.Record{
		Repository:       repository.String(),
		Package:          packageName,
		Version:          version,
		Tag:              "v" + version,
		Asset:            assetName,
		AssetDigest:      digest.String(),
		StorePath:        storePath,
		ArtifactPath:     artifactPath,
		ExtractedPath:    extractedPath,
		VerificationPath: verificationPath,
		Binaries: []state.Binary{
			{Name: packageName, LinkPath: linkPath, TargetPath: targetPath},
		},
		InstalledAt: time.Unix(1700000000, 0).UTC(),
	}
}

func assertSingleVerifyFailure(t *testing.T, results []VerifyInstalledResult, err error, contains string) {
	t.Helper()

	require.Error(t, err)
	var incomplete VerifyIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	require.Len(t, results, 1)
	assert.Equal(t, VerifyStatusCannotVerify, results[0].Status)
	assert.Contains(t, results[0].Reason, contains)
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

func (f fakeVerifyArchiveExtractor) MaterializeBinaries(_ context.Context, request ArtifactMaterializationRequest) ([]MaterializedBinary, error) {
	if err := os.MkdirAll(request.DestinationDir, 0o755); err != nil {
		return nil, err
	}
	out := make([]MaterializedBinary, 0, len(request.Binaries))
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
		out = append(out, MaterializedBinary{
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

func mustStateIndex(t *testing.T, records ...state.Record) state.Index {
	t.Helper()

	index := state.NewIndex()
	var err error
	for _, record := range records {
		index, err = index.AddRecord(record)
		require.NoError(t, err)
	}
	return index
}

func mustTestDigest(t *testing.T, pair string) verification.Digest {
	t.Helper()
	digest, err := verification.NewDigest("sha256", strings.Repeat(pair, 32))
	require.NoError(t, err)
	return digest
}
