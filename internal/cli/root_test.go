package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/spf13/viper"

	"github.com/meigma/ghd/internal/adapters/filesystem"
	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/config"
	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"ghd": runTestCommand,
	}))
}

func TestCLI(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
	})
}

func runTestCommand() int {
	vp := viper.New()
	root := NewRootCommand(Options{
		Out:   os.Stdout,
		Err:   os.Stderr,
		Viper: vp,
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			return testRuntime{}, nil
		},
	})
	root.SetArgs(os.Args[1:])
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

type testRuntime struct{}

func (testRuntime) AddRepository(ctx context.Context, request app.RepositoryAddRequest) (catalog.RepositoryRecord, error) {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return subject.AddRepository(ctx, request)
}

func (testRuntime) ListRepositories(ctx context.Context, indexDir string) ([]catalog.RepositoryRecord, error) {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return nil, err
	}
	return subject.ListRepositories(ctx, indexDir)
}

func (testRuntime) RemoveRepository(ctx context.Context, request app.RepositoryRemoveRequest) error {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return err
	}
	return subject.RemoveRepository(ctx, request)
}

func (testRuntime) RefreshRepositories(ctx context.Context, request app.RepositoryRefreshRequest) ([]catalog.RepositoryRecord, error) {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return nil, err
	}
	return subject.RefreshRepositories(ctx, request)
}

func (testRuntime) ResolvePackage(ctx context.Context, request app.ResolvePackageRequest) (app.ResolvePackageResult, error) {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return app.ResolvePackageResult{}, err
	}
	return subject.ResolvePackage(ctx, request)
}

func (testRuntime) ListPackages(ctx context.Context, request app.PackageListRequest) ([]app.PackageListResult, error) {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return nil, err
	}
	return subject.ListPackages(ctx, request)
}

func (testRuntime) InfoPackage(ctx context.Context, request app.PackageInfoRequest) (app.PackageInfoResult, error) {
	subject, err := newTestRepositoryCatalog()
	if err != nil {
		return app.PackageInfoResult{}, err
	}
	return subject.InfoPackage(ctx, request)
}

func (testRuntime) CheckInstalled(ctx context.Context, request app.CheckRequest) ([]app.CheckResult, error) {
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return nil, err
	}

	var records []state.Record
	if request.All {
		records = index.Normalize().Records
	} else {
		record, err := index.ResolveTarget(request.Target)
		if err != nil {
			return nil, err
		}
		records = []state.Record{record}
	}

	results := make([]app.CheckResult, 0, len(records))
	failures := 0
	for _, record := range records {
		result := app.CheckResult{
			Repository: record.Repository,
			Package:    record.Package,
			Version:    record.Version,
		}
		switch record.Repository {
		case "owner/repo":
			result.Status = app.CheckStatusUpdateAvailable
			result.LatestVersion = "1.3.0"
		case "owner/current":
			result.Status = app.CheckStatusUpToDate
		default:
			result.Status = app.CheckStatusCannotDetermine
			result.Reason = "fetch ghd.toml: missing"
			failures++
		}
		results = append(results, result)
	}
	if failures != 0 {
		return results, app.CheckIncompleteError{Failed: failures}
	}
	return results, nil
}

func (testRuntime) Update(ctx context.Context, request app.UpdateRequest) (app.UpdateResult, error) {
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return app.UpdateResult{}, err
	}
	previous, err := index.ResolveTarget(request.Target)
	if err != nil {
		return app.UpdateResult{}, err
	}
	previousBinaries := make([]app.InstalledBinary, 0, len(previous.Binaries))
	for _, binary := range previous.Binaries {
		previousBinaries = append(previousBinaries, app.InstalledBinary{
			Name:       binary.Name,
			LinkPath:   binary.LinkPath,
			TargetPath: binary.TargetPath,
		})
	}
	if previous.Version == "1.3.0" {
		return app.UpdateResult{
			Previous: previous,
			Current:  previous,
			Updated:  false,
			Binaries: previousBinaries,
		}, nil
	}

	storeRoot, err := filepath.Abs(filepath.Clean(request.StoreDir))
	if err != nil {
		return app.UpdateResult{}, err
	}
	binRoot, err := filepath.Abs(filepath.Clean(request.BinDir))
	if err != nil {
		return app.UpdateResult{}, err
	}
	newVersion := "1.3.0"
	newStorePath := filepath.Join(
		storeRoot,
		"github.com",
		"owner",
		"repo",
		previous.Package,
		newVersion,
		"sha256-abc123",
	)
	newExtractedPath := filepath.Join(newStorePath, "extracted")
	newArtifactPath := filepath.Join(newStorePath, "artifact")
	newVerificationPath := filepath.Join(newStorePath, "verification.json")
	if err := os.MkdirAll(newExtractedPath, 0o755); err != nil {
		return app.UpdateResult{}, err
	}
	newTargetPath := filepath.Join(newExtractedPath, previous.Package)
	if err := os.WriteFile(newTargetPath, []byte("binary"), 0o755); err != nil {
		return app.UpdateResult{}, err
	}
	if err := os.WriteFile(newArtifactPath, []byte("artifact"), 0o600); err != nil {
		return app.UpdateResult{}, err
	}
	if err := writeTestVerificationRecord(newVerificationPath, verification.Repository{Owner: "owner", Name: "repo"}, previous.Package, newVersion); err != nil {
		return app.UpdateResult{}, err
	}
	nextBinaries := []app.InstalledBinary{
		{
			Name:       previous.Package,
			LinkPath:   filepath.Join(binRoot, previous.Package),
			TargetPath: newTargetPath,
		},
	}
	installer := filesystem.NewInstaller()
	if err := installer.ReplaceManagedBinaries(ctx, app.ReplaceManagedBinariesRequest{
		BinDir:   binRoot,
		Previous: previousBinaries,
		Next:     nextBinaries,
	}); err != nil {
		return app.UpdateResult{}, err
	}
	current := previous
	current.Version = newVersion
	current.Tag = "v" + newVersion
	current.StorePath = newStorePath
	current.ArtifactPath = newArtifactPath
	current.ExtractedPath = newExtractedPath
	current.VerificationPath = newVerificationPath
	current.AssetDigest = "sha256:" + strings.Repeat("a", 64)
	current.Binaries = []state.Binary{
		{Name: previous.Package, LinkPath: nextBinaries[0].LinkPath, TargetPath: nextBinaries[0].TargetPath},
	}
	current.InstalledAt = time.Unix(1700000100, 0).UTC()
	if _, err := store.ReplaceInstalledRecord(ctx, request.StateDir, current); err != nil {
		return app.UpdateResult{}, err
	}
	if err := installer.RemoveManagedStore(ctx, storeRoot, previous.StorePath); err != nil {
		return app.UpdateResult{}, err
	}
	return app.UpdateResult{
		Previous: previous,
		Current:  current,
		Updated:  true,
		Binaries: nextBinaries,
	}, nil
}

func (testRuntime) ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error) {
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	return index.Normalize().Records, nil
}

func (testRuntime) VerifyInstalled(ctx context.Context, request app.VerifyInstalledRequest) (state.Record, error) {
	subject, err := app.NewInstalledPackageVerifier(app.InstalledPackageVerifierDependencies{
		StateStore:    filesystem.NewInstalledStore(),
		Verifier:      testReleaseVerifier{},
		EvidenceStore: filesystem.NewEvidenceWriter(),
		Archives:      testArchiveExtractor{},
		FileSystem:    filesystem.NewInstaller(),
	})
	if err != nil {
		return state.Record{}, err
	}
	return subject.Verify(ctx, request)
}

func (testRuntime) Uninstall(ctx context.Context, request app.UninstallRequest) (state.Record, error) {
	subject, err := app.NewPackageUninstaller(app.PackageUninstallerDependencies{
		StateStore: filesystem.NewInstalledStore(),
		FileSystem: filesystem.NewInstaller(),
	})
	if err != nil {
		return state.Record{}, err
	}
	return subject.Uninstall(ctx, request)
}

func (testRuntime) Download(_ context.Context, request app.VerifiedDownloadRequest) (app.VerifiedDownloadResult, error) {
	artifactPath := filepath.Join(request.OutputDir, "artifact.tar.gz")
	evidencePath := filepath.Join(request.OutputDir, "verification.json")
	if err := os.MkdirAll(request.OutputDir, 0o755); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o600); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	if err := os.WriteFile(evidencePath, []byte("{}\n"), 0o600); err != nil {
		return app.VerifiedDownloadResult{}, err
	}
	return app.VerifiedDownloadResult{
		Repository:   request.Repository,
		PackageName:  request.PackageName,
		Version:      request.Version,
		ArtifactPath: artifactPath,
		EvidencePath: evidencePath,
	}, nil
}

func (testRuntime) Install(ctx context.Context, request app.VerifiedInstallRequest) (app.VerifiedInstallResult, error) {
	binDir, err := filepath.Abs(filepath.Clean(request.BinDir))
	if err != nil {
		return app.VerifiedInstallResult{}, err
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	storeRoot, err := filepath.Abs(filepath.Clean(request.StoreDir))
	if err != nil {
		return app.VerifiedInstallResult{}, err
	}
	storePath := filepath.Join(
		storeRoot,
		"github.com",
		request.Repository.Owner,
		request.Repository.Name,
		request.PackageName,
		request.Version,
		"sha256-abc123",
	)
	extractedPath := filepath.Join(storePath, "extracted")
	if err := os.MkdirAll(extractedPath, 0o755); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	linkPath := filepath.Join(binDir, request.PackageName)
	targetPath := filepath.Join(extractedPath, request.PackageName)
	linkTarget := targetPath
	artifactPath := filepath.Join(storePath, "artifact")
	verificationPath := filepath.Join(storePath, "verification.json")
	if err := os.WriteFile(targetPath, []byte("binary"), 0o755); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o600); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	if err := writeTestVerificationRecord(verificationPath, request.Repository, request.PackageName, request.Version); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	if err := os.Symlink(linkTarget, linkPath); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	record := state.Record{
		Repository:       request.Repository.String(),
		Package:          request.PackageName,
		Version:          request.Version,
		Tag:              "v" + request.Version,
		Asset:            request.PackageName + ".tar.gz",
		AssetDigest:      "sha256:" + strings.Repeat("a", 64),
		StorePath:        storePath,
		ArtifactPath:     artifactPath,
		ExtractedPath:    extractedPath,
		VerificationPath: verificationPath,
		Binaries:         []state.Binary{{Name: request.PackageName, LinkPath: linkPath, TargetPath: linkTarget}},
		InstalledAt:      time.Unix(1700000000, 0).UTC(),
	}
	store := filesystem.NewInstalledStore()
	if _, err := store.AddInstalledRecord(ctx, request.StateDir, record); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	return app.VerifiedInstallResult{
		Repository:  request.Repository,
		PackageName: request.PackageName,
		Version:     request.Version,
		Binaries: []app.InstalledBinary{
			{Name: request.PackageName, LinkPath: linkPath, TargetPath: linkTarget},
		},
	}, nil
}

func (testRuntime) Doctor(ctx context.Context, request app.DoctorRequest) ([]app.DoctorResult, error) {
	subject, err := app.NewEnvironmentDoctor(app.EnvironmentDoctorDependencies{
		GitHub:      testGitHubDoctorChecker{token: request.GitHubToken},
		TrustedRoot: testTrustedRootChecker{},
	})
	if err != nil {
		return nil, err
	}
	return subject.Doctor(ctx, request)
}

func newTestRepositoryCatalog() (*app.RepositoryCatalog, error) {
	return app.NewRepositoryCatalog(app.RepositoryCatalogDependencies{
		Manifests: testRuntimeManifestSource{},
		Store:     filesystem.NewCatalogStore(),
		Now:       func() time.Time { return time.Unix(1700000000, 0) },
	})
}

type testRuntimeManifestSource struct{}

func (testRuntimeManifestSource) FetchManifest(_ context.Context, repository verification.Repository) ([]byte, error) {
	cfg, err := testManifestConfig(repository)
	if err != nil {
		return nil, err
	}
	return toml.Marshal(cfg)
}

func testManifestConfig(repository verification.Repository) (manifest.Config, error) {
	switch repository.Name {
	case "binary":
		return manifest.Config{
			Version: manifest.SchemaVersion,
			Provenance: manifest.Provenance{
				SignerWorkflow: repository.String() + "/.github/workflows/release.yml",
			},
			Packages: []manifest.Package{
				{
					Name:        "bar",
					Description: "Bar CLI",
					Assets: []manifest.Asset{
						{OS: "darwin", Arch: "arm64", Pattern: "bar_${version}_darwin_arm64.tar.gz"},
						{OS: "linux", Arch: "amd64", Pattern: "bar_${version}_linux_amd64.tar.gz"},
					},
					Binaries: []manifest.Binary{{Path: "bin/foo"}},
				},
			},
		}, nil
	case "multi":
		return manifest.Config{
			Version: manifest.SchemaVersion,
			Provenance: manifest.Provenance{
				SignerWorkflow: repository.String() + "/.github/workflows/release.yml",
			},
			Packages: []manifest.Package{
				{
					Name:        "foo",
					Description: "Foo CLI",
					Assets: []manifest.Asset{
						{OS: "darwin", Arch: "arm64", Pattern: "foo_${version}_darwin_arm64.tar.gz"},
						{OS: "linux", Arch: "amd64", Pattern: "foo_${version}_linux_amd64.tar.gz"},
					},
					Binaries: []manifest.Binary{{Path: "bin/foo"}},
				},
				{
					Name:        "bar",
					Description: "Bar CLI",
					TagPattern:  "bar-v${version}",
					Assets: []manifest.Asset{
						{OS: "darwin", Arch: "arm64", Pattern: "bar_${version}_darwin_arm64.tar.gz"},
						{OS: "linux", Arch: "amd64", Pattern: "bar_${version}_linux_amd64.tar.gz"},
					},
					Binaries: []manifest.Binary{{Path: "bin/bar"}},
				},
			},
		}, nil
	default:
		return manifest.Config{
			Version: manifest.SchemaVersion,
			Provenance: manifest.Provenance{
				SignerWorkflow: repository.String() + "/.github/workflows/release.yml",
			},
			Packages: []manifest.Package{
				{
					Name:        "foo",
					Description: "Foo CLI",
					Assets: []manifest.Asset{
						{OS: "darwin", Arch: "arm64", Pattern: "foo_${version}_darwin_arm64.tar.gz"},
						{OS: "linux", Arch: "amd64", Pattern: "foo_${version}_linux_amd64.tar.gz"},
					},
					Binaries: []manifest.Binary{{Path: "bin/foo"}},
				},
			},
		}, nil
	}
}

type testReleaseVerifier struct{}

func (testReleaseVerifier) VerifyReleaseAsset(_ context.Context, request verification.Request) (verification.Evidence, error) {
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	if err != nil {
		return verification.Evidence{}, err
	}
	return verification.Evidence{
		Repository:  request.Repository,
		Tag:         request.Tag,
		AssetDigest: digest,
		ProvenanceAttestation: verification.AttestationEvidence{
			SignerWorkflow: request.Policy.TrustedSignerWorkflow,
		},
	}, nil
}

type testArchiveExtractor struct{}

func (testArchiveExtractor) ExtractArchive(_ context.Context, request app.ArchiveExtractionRequest) ([]app.ExtractedBinary, error) {
	if err := os.MkdirAll(request.DestinationDir, 0o755); err != nil {
		return nil, err
	}
	out := make([]app.ExtractedBinary, 0, len(request.Binaries))
	for _, binary := range request.Binaries {
		targetPath := filepath.Join(request.DestinationDir, filepath.FromSlash(binary.Path))
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(targetPath, []byte("binary"), 0o755); err != nil {
			return nil, err
		}
		out = append(out, app.ExtractedBinary{
			Name:         filepath.Base(binary.Path),
			RelativePath: binary.Path,
			Path:         targetPath,
		})
	}
	return out, nil
}

type testGitHubDoctorChecker struct {
	token string
}

func (c testGitHubDoctorChecker) CheckRateLimit(context.Context) (app.GitHubRateLimitStatus, error) {
	switch strings.TrimSpace(c.token) {
	case "fail":
		return app.GitHubRateLimitStatus{}, fmt.Errorf("boom")
	case "exhausted":
		return app.GitHubRateLimitStatus{CoreLimit: 60, CoreRemaining: 0}, nil
	case "":
		return app.GitHubRateLimitStatus{CoreLimit: 60, CoreRemaining: 42}, nil
	default:
		return app.GitHubRateLimitStatus{CoreLimit: 5000, CoreRemaining: 4999, CoreUsed: 1}, nil
	}
}

type testTrustedRootChecker struct{}

func (testTrustedRootChecker) ValidateTrustedRoot(_ context.Context, path string) error {
	if strings.Contains(path, "invalid") {
		return fmt.Errorf("parse trusted root: bad root")
	}
	return nil
}

func writeTestVerificationRecord(path string, repository verification.Repository, packageName string, version string) error {
	outputDir := filepath.Dir(path)
	writer := filesystem.NewEvidenceWriter()
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	if err != nil {
		return err
	}
	_, err = writer.WriteVerificationEvidence(context.Background(), outputDir, app.VerificationRecord{
		SchemaVersion: 1,
		Repository:    repository.String(),
		Package:       packageName,
		Version:       version,
		Tag:           "v" + version,
		Asset:         packageName + ".tar.gz",
		Evidence: verification.Evidence{
			Repository:  repository,
			Tag:         verification.ReleaseTag("v" + version),
			AssetDigest: digest,
			ProvenanceAttestation: verification.AttestationEvidence{
				SignerWorkflow: verification.WorkflowIdentity(repository.String() + "/.github/workflows/release.yml"),
			},
		},
	})
	return err
}
