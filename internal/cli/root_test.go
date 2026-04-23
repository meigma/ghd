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
	index, err := store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	index, err = index.UpsertRepository(record)
	if err != nil {
		return catalog.RepositoryRecord{}, err
	}
	if err := store.SaveCatalog(ctx, request.IndexDir, index); err != nil {
		return catalog.RepositoryRecord{}, err
	}
	return record, nil
}

func (testRuntime) ListRepositories(ctx context.Context, request app.RepositoryListRequest) (app.RepositoryListResult, error) {
	store := filesystem.NewCatalogStore()
	index, err := store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return app.RepositoryListResult{}, err
	}
	return app.RepositoryListResult{Repositories: index.Normalize().Repositories}, nil
}

func (testRuntime) RemoveRepository(ctx context.Context, request app.RepositoryRemoveRequest) error {
	store := filesystem.NewCatalogStore()
	index, err := store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return err
	}
	index, removed := index.RemoveRepository(request.Repository)
	if !removed {
		return fmt.Errorf("repository %s is not indexed", request.Repository)
	}
	return store.SaveCatalog(ctx, request.IndexDir, index)
}

func (testRuntime) RefreshRepositories(ctx context.Context, request app.RepositoryRefreshRequest) (app.RepositoryRefreshResult, error) {
	store := filesystem.NewCatalogStore()
	index, err := store.LoadCatalog(ctx, request.IndexDir)
	if err != nil {
		return app.RepositoryRefreshResult{}, err
	}
	if !request.Repository.IsZero() {
		if _, ok := index.Repository(request.Repository); !ok {
			return app.RepositoryRefreshResult{}, fmt.Errorf("repository %s is not indexed", request.Repository)
		}
		record, err := testRepositoryRecord(request.Repository)
		if err != nil {
			return app.RepositoryRefreshResult{}, err
		}
		index, err = index.UpsertRepository(record)
		if err != nil {
			return app.RepositoryRefreshResult{}, err
		}
		if err := store.SaveCatalog(ctx, request.IndexDir, index); err != nil {
			return app.RepositoryRefreshResult{}, err
		}
		return app.RepositoryRefreshResult{Repositories: []catalog.RepositoryRecord{record}}, nil
	}
	refreshed := make([]catalog.RepositoryRecord, 0, len(index.Repositories))
	for _, existing := range index.Normalize().Repositories {
		record, err := testRepositoryRecord(existing.Repository)
		if err != nil {
			return app.RepositoryRefreshResult{}, err
		}
		index, err = index.UpsertRepository(record)
		if err != nil {
			return app.RepositoryRefreshResult{}, err
		}
		refreshed = append(refreshed, record)
	}
	if err := store.SaveCatalog(ctx, request.IndexDir, index); err != nil {
		return app.RepositoryRefreshResult{}, err
	}
	return app.RepositoryRefreshResult{Repositories: refreshed}, nil
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

func (testRuntime) ListInstalled(ctx context.Context, request app.InstalledListRequest) (app.InstalledListResult, error) {
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return app.InstalledListResult{}, err
	}
	return app.InstalledListResult{Records: index.Normalize().Records}, nil
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
	if err := os.MkdirAll(request.BinDir, 0o755); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	if err := os.MkdirAll(request.StoreDir, 0o755); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	linkPath := filepath.Join(request.BinDir, request.PackageName)
	if err := os.WriteFile(linkPath, []byte("binary"), 0o755); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	record := state.Record{
		Repository:       request.Repository.String(),
		Package:          request.PackageName,
		Version:          request.Version,
		Tag:              "v" + request.Version,
		Asset:            request.PackageName + ".tar.gz",
		AssetDigest:      "sha256:abc123",
		StorePath:        request.StoreDir,
		ArtifactPath:     filepath.Join(request.StoreDir, "artifact"),
		ExtractedPath:    filepath.Join(request.StoreDir, "extracted"),
		VerificationPath: filepath.Join(request.StoreDir, "verification.json"),
		Binaries:         []state.Binary{{Name: request.PackageName, LinkPath: linkPath, TargetPath: filepath.Join(request.StoreDir, "artifact")}},
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
			{Name: request.PackageName, LinkPath: linkPath, TargetPath: filepath.Join(request.StoreDir, "artifact")},
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
