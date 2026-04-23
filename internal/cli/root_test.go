package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	record, err := testRepositoryRecord(request.Repository)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	store := filesystem.NewCatalogStore()
	if _, err := store.UpsertRepository(ctx, request.IndexDir, record); err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return record, nil
}

func (testRuntime) ListRepositories(ctx context.Context, indexDir string) ([]catalog.RepositoryRecord, error) {
	store := filesystem.NewCatalogStore()
	index, err := store.LoadCatalog(ctx, indexDir)
	if err != nil {
		return nil, err
	}
	return index.Normalize().Repositories, nil
}

func (testRuntime) RemoveRepository(ctx context.Context, request app.RepositoryRemoveRequest) error {
	store := filesystem.NewCatalogStore()
	_, err := store.RemoveRepository(ctx, request.IndexDir, request.Repository)
	return err
}

func (testRuntime) RefreshRepositories(ctx context.Context, request app.RepositoryRefreshRequest) ([]catalog.RepositoryRecord, error) {
	store := filesystem.NewCatalogStore()
	index, err := store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return nil, err
	}
	if !request.Repository.IsZero() {
		if _, ok := index.Repository(request.Repository); !ok {
			return nil, fmt.Errorf("repository %s is not indexed", request.Repository)
		}
		record, err := testRepositoryRecord(request.Repository)
		if err != nil {
			return nil, err
		}
		if _, err := store.UpsertRepository(ctx, request.IndexDir, record); err != nil {
			return nil, err
		}
		return []catalog.RepositoryRecord{record}, nil
	}
	refreshed := make([]catalog.RepositoryRecord, 0, len(index.Repositories))
	for _, existing := range index.Normalize().Repositories {
		record, err := testRepositoryRecord(existing.Repository)
		if err != nil {
			return nil, err
		}
		refreshed = append(refreshed, record)
	}
	if _, err := store.UpsertRepositories(ctx, request.IndexDir, refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func (testRuntime) ResolvePackage(ctx context.Context, request app.ResolvePackageRequest) (app.ResolvePackageResult, error) {
	store := filesystem.NewCatalogStore()
	index, err := store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return app.ResolvePackageResult{}, err
	}
	resolved, err := index.ResolvePackage(request.PackageName)
	if err != nil {
		return app.ResolvePackageResult{}, err
	}
	return app.ResolvePackageResult{Repository: resolved.Repository, PackageName: resolved.PackageName}, nil
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

func (testRuntime) ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error) {
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	return index.Normalize().Records, nil
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
	if err := os.WriteFile(verificationPath, []byte("{}\n"), 0o600); err != nil {
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
		AssetDigest:      "sha256:abc123",
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

func testRepositoryRecord(repository verification.Repository) (catalog.RepositoryRecord, error) {
	packageName := "foo"
	binaryPath := "bin/foo"
	if repository.Name == "binary" {
		packageName = "bar"
		binaryPath = "bin/foo"
	}
	return catalog.NewRepositoryRecord(repository, manifest.Config{
		Version: manifest.SchemaVersion,
		Provenance: manifest.Provenance{
			SignerWorkflow: repository.String() + "/.github/workflows/release.yml",
		},
		Packages: []manifest.Package{
			{Name: packageName, Description: "Foo CLI", Binaries: []manifest.Binary{{Path: binaryPath}}},
		},
	}, time.Unix(1700000000, 0))
}
