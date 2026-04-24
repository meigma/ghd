package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestVerifyWithoutTargetFailsBeforeRuntimeSetup(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var called bool

	root := NewRootCommand(Options{
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			called = true
			return nil, fmt.Errorf("runtime should not be constructed")
		},
	})
	root.SetArgs([]string{"verify"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected missing target error")
	}
	if err.Error() != "verify target must be set" {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatalf("runtime factory should not have been called")
	}
}

func TestUpdateWithoutTargetFailsBeforeRuntimeSetup(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var called bool

	root := NewRootCommand(Options{
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			called = true
			return nil, fmt.Errorf("runtime should not be constructed")
		},
	})
	root.SetArgs([]string{"update", "--store-dir", "/tmp/ghd-store", "--bin-dir", "/tmp/ghd-bin"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected missing target error")
	}
	if err.Error() != "update target must be set" {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatalf("runtime factory should not have been called")
	}
}

func TestInstallYesNonInteractiveKeepsPlainOutput(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	root := NewRootCommand(Options{
		In:    strings.NewReader(""),
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			return testRuntime{}, nil
		},
	})
	root.SetArgs([]string{
		"--state-dir", stateDir,
		"--yes",
		"--non-interactive",
		"install", "owner/repo/foo@1.2.3",
		"--store-dir", storeDir,
		"--bin-dir", binDir,
	})

	err := root.ExecuteContext(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "installed owner/repo/foo@1.2.3\n", stderr.String())
	assert.Equal(t, fmt.Sprintf("binary %s\n", filepath.Join(binDir, "foo")), stdout.String())
}

func TestInstallInteractiveDoesNotWriteBinaryStdout(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	root := NewRootCommand(Options{
		In:    strings.NewReader(""),
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			return testRuntime{}, nil
		},
		InstallConfirmation: func(context.Context, app.InstallApproval) error {
			return nil
		},
	})
	root.SetArgs([]string{
		"--state-dir", stateDir,
		"install", "owner/repo/foo@1.2.3",
		"--store-dir", storeDir,
		"--bin-dir", binDir,
	})

	err := root.ExecuteContext(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "installed owner/repo/foo@1.2.3\n", stderr.String())
	assert.Empty(t, stdout.String())
	assert.FileExists(t, filepath.Join(binDir, "foo"))
}

func TestInstallNonInteractiveWithoutYesFailsAfterVerification(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	root := NewRootCommand(Options{
		In:    strings.NewReader(""),
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			return testRuntime{}, nil
		},
	})
	root.SetArgs([]string{
		"--state-dir", stateDir,
		"--non-interactive",
		"install", "owner/repo/foo@1.2.3",
		"--store-dir", storeDir,
		"--bin-dir", binDir,
	})

	err := root.ExecuteContext(context.Background())

	require.Error(t, err)
	assert.Equal(t, "install requires approval after verification; rerun with --yes to approve non-interactively", err.Error())
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
	assert.NoFileExists(t, filepath.Join(binDir, "foo"))
}

func TestInstallApprovalDeclineDoesNotMutateState(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	root := NewRootCommand(Options{
		In:    strings.NewReader(""),
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			return testRuntime{}, nil
		},
		InstallConfirmation: func(context.Context, app.InstallApproval) error {
			return app.ErrInstallNotApproved
		},
	})
	root.SetArgs([]string{
		"--state-dir", stateDir,
		"install", "owner/repo/foo@1.2.3",
		"--store-dir", storeDir,
		"--bin-dir", binDir,
	})

	err := root.ExecuteContext(context.Background())

	require.ErrorIs(t, err, app.ErrInstallNotApproved)
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
	assert.NoFileExists(t, filepath.Join(binDir, "foo"))
	assert.NoFileExists(t, filepath.Join(stateDir, "installed.json"))
}

func TestInstallApprovalFactsIncludeVerificationFields(t *testing.T) {
	t.Helper()

	var approval app.InstallApproval
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	root := NewRootCommand(Options{
		In:    strings.NewReader(""),
		Out:   io.Discard,
		Err:   io.Discard,
		Viper: viper.New(),
		RuntimeFactory: func(context.Context, config.Config) (Runtime, error) {
			return testRuntime{}, nil
		},
		InstallConfirmation: func(_ context.Context, got app.InstallApproval) error {
			approval = got
			return nil
		},
	})
	root.SetArgs([]string{
		"--state-dir", stateDir,
		"install", "owner/repo/foo@1.2.3",
		"--store-dir", storeDir,
		"--bin-dir", binDir,
	})

	err := root.ExecuteContext(context.Background())

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, approval.Repository)
	assert.Equal(t, "foo", approval.PackageName)
	assert.Equal(t, "1.2.3", approval.Version)
	assert.Equal(t, verification.ReleaseTag("v1.2.3"), approval.Tag)
	assert.Equal(t, "foo.tar.gz", approval.AssetName)
	assert.Equal(t, "sha256:"+strings.Repeat("a", 64), approval.AssetDigest.String())
	assert.Equal(t, verification.ReleasePredicateV02, approval.ReleasePredicateType)
	assert.Equal(t, verification.SLSAPredicateV1, approval.ProvenancePredicateType)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), approval.SignerWorkflow)
	assert.Equal(t, binDir, approval.BinDir)
	assert.Equal(t, []string{"foo"}, approval.Binaries)
}

func TestUpdateYesNonInteractiveKeepsPlainRows(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	require.NoError(t, executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--yes", "--non-interactive", "install", "owner/repo/foo@1.2.3", "--store-dir", storeDir, "--bin-dir", binDir))
	stdout.Reset()
	stderr.Reset()

	err := executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--yes", "--non-interactive", "update", "foo", "--store-dir", storeDir, "--bin-dir", binDir)

	require.NoError(t, err)
	assert.Equal(t, "owner/repo/foo 1.2.3 1.3.0 updated\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestUpdateNonInteractiveWithoutYesReturnsResultRow(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	require.NoError(t, executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--yes", "--non-interactive", "install", "owner/repo/foo@1.2.3", "--store-dir", storeDir, "--bin-dir", binDir))
	stdout.Reset()
	stderr.Reset()

	err := executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--non-interactive", "update", "foo", "--store-dir", storeDir, "--bin-dir", binDir)

	require.Error(t, err)
	assert.Equal(t, "could not update 1 installed package", err.Error())
	assert.Equal(t, "owner/repo/foo 1.2.3 1.2.3 cannot-update update requires approval after verification; rerun with --yes to approve non-interactively\n", stdout.String())
	assert.Empty(t, stderr.String())
	record := requireInstalledRecord(t, stateDir, "owner/repo", "foo")
	assert.Equal(t, "1.2.3", record.Version)
}

func TestUpdateJSONWithoutYesReturnsStructuredCannotUpdate(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	require.NoError(t, executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--yes", "--non-interactive", "install", "owner/repo/foo@1.2.3", "--store-dir", storeDir, "--bin-dir", binDir))
	stdout.Reset()
	stderr.Reset()

	err := executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "update", "foo", "--json", "--store-dir", storeDir, "--bin-dir", binDir)

	require.Error(t, err)
	assert.Equal(t, "could not update 1 installed package", err.Error())
	assert.Contains(t, stdout.String(), `"updates":[`)
	assert.Contains(t, stdout.String(), `"target":"owner/repo/foo"`)
	assert.Contains(t, stdout.String(), `"previous_version":"1.2.3"`)
	assert.Contains(t, stdout.String(), `"current_version":"1.2.3"`)
	assert.Contains(t, stdout.String(), `"status":"cannot-update"`)
	assert.Contains(t, stdout.String(), `"reason":"update requires approval after verification; rerun with --yes to approve non-interactively"`)
	assert.Empty(t, stderr.String())
}

func TestUpdateInteractiveApprovalCanApprove(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var approval app.UpdateApproval
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	require.NoError(t, executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--yes", "--non-interactive", "install", "owner/repo/foo@1.2.3", "--store-dir", storeDir, "--bin-dir", binDir))
	stdout.Reset()
	stderr.Reset()

	err := executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
		UpdateConfirmation: func(_ context.Context, got app.UpdateApproval) error {
			approval = got
			return nil
		},
	}, "--state-dir", stateDir, "update", "foo", "--store-dir", storeDir, "--bin-dir", binDir)

	require.NoError(t, err)
	assert.Equal(t, "owner/repo/foo 1.2.3 1.3.0 updated\n", stdout.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, approval.Repository)
	assert.Equal(t, "foo", approval.PackageName)
	assert.Equal(t, "1.2.3", approval.PreviousVersion)
	assert.Equal(t, "1.3.0", approval.Version)
	assert.Equal(t, verification.ReleaseTag("v1.3.0"), approval.Tag)
	assert.Equal(t, "foo.tar.gz", approval.AssetName)
	assert.Equal(t, "sha256:"+strings.Repeat("a", 64), approval.AssetDigest.String())
	assert.Equal(t, verification.ReleasePredicateV02, approval.ReleasePredicateType)
	assert.Equal(t, verification.SLSAPredicateV1, approval.ProvenancePredicateType)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), approval.SignerWorkflow)
	assert.Equal(t, binDir, approval.BinDir)
	assert.Equal(t, []string{"foo"}, approval.Binaries)
}

func TestUpdateApprovalDeclineDoesNotMutateState(t *testing.T) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stateDir := t.TempDir()
	storeDir := t.TempDir()
	binDir := t.TempDir()
	require.NoError(t, executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
	}, "--state-dir", stateDir, "--yes", "--non-interactive", "install", "owner/repo/foo@1.2.3", "--store-dir", storeDir, "--bin-dir", binDir))
	stdout.Reset()
	stderr.Reset()

	err := executeTestRoot(Options{
		In:             strings.NewReader(""),
		Out:            &stdout,
		Err:            &stderr,
		RuntimeFactory: testRuntimeFactory,
		UpdateConfirmation: func(context.Context, app.UpdateApproval) error {
			return app.ErrUpdateNotApproved
		},
	}, "--state-dir", stateDir, "update", "foo", "--store-dir", storeDir, "--bin-dir", binDir)

	require.Error(t, err)
	assert.Equal(t, "could not update 1 installed package", err.Error())
	assert.Equal(t, "owner/repo/foo 1.2.3 1.2.3 cannot-update update was not approved\n", stdout.String())
	assert.Empty(t, stderr.String())
	record := requireInstalledRecord(t, stateDir, "owner/repo", "foo")
	assert.Equal(t, "1.2.3", record.Version)
}

func runTestCommand() int {
	vp := viper.New()
	root := NewRootCommand(Options{
		In:    os.Stdin,
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

func executeTestRoot(options Options, args ...string) error {
	if options.Viper == nil {
		options.Viper = viper.New()
	}
	root := NewRootCommand(options)
	root.SetArgs(args)
	return root.ExecuteContext(context.Background())
}

func testRuntimeFactory(context.Context, config.Config) (Runtime, error) {
	return testRuntime{}, nil
}

func requireInstalledRecord(t *testing.T, stateDir string, repository string, packageName string) state.Record {
	t.Helper()

	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(context.Background(), stateDir)
	require.NoError(t, err)
	record, ok := index.Record(repository, packageName)
	require.True(t, ok)
	return record
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

func (testRuntime) Update(ctx context.Context, request app.UpdateRequest) ([]app.UpdateInstalledResult, error) {
	if request.All && strings.TrimSpace(request.Target) != "" {
		return nil, fmt.Errorf("update accepts a target or --all, not both")
	}
	if !request.All && strings.TrimSpace(request.Target) == "" {
		return nil, fmt.Errorf("update target must be set")
	}
	if strings.TrimSpace(request.StoreDir) == "" {
		return nil, fmt.Errorf("store directory must be set")
	}
	if strings.TrimSpace(request.BinDir) == "" {
		return nil, fmt.Errorf("bin directory must be set")
	}
	if strings.TrimSpace(request.StateDir) == "" {
		return nil, fmt.Errorf("state directory must be set")
	}

	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return nil, err
	}
	var records []state.Record
	if request.All {
		records = index.Normalize().Records
	} else {
		previous, err := index.ResolveTarget(request.Target)
		if err != nil {
			return nil, err
		}
		records = []state.Record{previous}
	}

	results := make([]app.UpdateInstalledResult, 0, len(records))
	failed := 0
	warned := 0
	for _, previous := range records {
		result, err := updateTestRecord(ctx, store, request, previous)
		results = append(results, result)
		switch result.Status {
		case app.UpdateStatusCannotUpdate:
			failed++
		case app.UpdateStatusUpdatedWithWarning:
			warned++
		}
		if err != nil {
			continue
		}
	}
	if failed != 0 || warned != 0 {
		return results, app.UpdateIncompleteError{Failed: failed, Warned: warned}
	}
	return results, nil
}

func updateTestRecord(ctx context.Context, store filesystem.InstalledStore, request app.UpdateRequest, previous state.Record) (app.UpdateInstalledResult, error) {
	if previous.Repository == "owner/broken" {
		return app.UpdateInstalledResult{
			Repository:      previous.Repository,
			Package:         previous.Package,
			PreviousVersion: previous.Version,
			CurrentVersion:  previous.Version,
			Status:          app.UpdateStatusCannotUpdate,
			Reason:          "fetch ghd.toml: missing",
		}, errors.New("fetch ghd.toml: missing")
	}
	if previous.Version == "1.3.0" {
		return app.UpdateInstalledResult{
			Repository:      previous.Repository,
			Package:         previous.Package,
			PreviousVersion: previous.Version,
			CurrentVersion:  previous.Version,
			Status:          app.UpdateStatusAlreadyUpToDate,
		}, nil
	}
	index, err := store.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return app.UpdateInstalledResult{}, err
	}
	owner := state.PackageRef{Repository: previous.Repository, Package: previous.Package}
	if err := index.CheckBinaryOwnership(owner, []string{previous.Package}, owner); err != nil {
		return app.UpdateInstalledResult{
			Repository:      previous.Repository,
			Package:         previous.Package,
			PreviousVersion: previous.Version,
			CurrentVersion:  previous.Version,
			Status:          app.UpdateStatusCannotUpdate,
			Reason:          err.Error(),
		}, err
	}
	previousBinaries := make([]app.InstalledBinary, 0, len(previous.Binaries))
	for _, binary := range previous.Binaries {
		previousBinaries = append(previousBinaries, app.InstalledBinary{
			Name:       binary.Name,
			LinkPath:   binary.LinkPath,
			TargetPath: binary.TargetPath,
		})
	}

	repository, err := testRepositoryFromString(previous.Repository)
	if err != nil {
		return app.UpdateInstalledResult{}, err
	}
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	if err != nil {
		return app.UpdateInstalledResult{}, err
	}
	newVersion := "1.3.0"
	if request.Approve != nil {
		if err := request.Approve(ctx, app.UpdateApproval{
			Repository:              repository,
			PackageName:             previous.Package,
			PreviousVersion:         previous.Version,
			Version:                 newVersion,
			Tag:                     verification.ReleaseTag("v" + newVersion),
			AssetName:               previous.Package + ".tar.gz",
			AssetDigest:             digest,
			ReleasePredicateType:    verification.ReleasePredicateV02,
			ProvenancePredicateType: verification.SLSAPredicateV1,
			SignerWorkflow:          verification.WorkflowIdentity(previous.Repository + "/.github/workflows/release.yml"),
			BinDir:                  request.BinDir,
			Binaries:                []string{previous.Package},
		}); err != nil {
			return app.UpdateInstalledResult{
				Repository:      previous.Repository,
				Package:         previous.Package,
				PreviousVersion: previous.Version,
				CurrentVersion:  previous.Version,
				Status:          app.UpdateStatusCannotUpdate,
				Reason:          err.Error(),
			}, err
		}
	}

	storeRoot, err := filepath.Abs(filepath.Clean(request.StoreDir))
	if err != nil {
		return app.UpdateInstalledResult{}, err
	}
	binRoot, err := filepath.Abs(filepath.Clean(request.BinDir))
	if err != nil {
		return app.UpdateInstalledResult{}, err
	}
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
		return app.UpdateInstalledResult{}, err
	}
	newTargetPath := filepath.Join(newExtractedPath, previous.Package)
	if err := os.WriteFile(newTargetPath, []byte("binary"), 0o755); err != nil {
		return app.UpdateInstalledResult{}, err
	}
	if err := os.WriteFile(newArtifactPath, []byte("artifact"), 0o600); err != nil {
		return app.UpdateInstalledResult{}, err
	}
	if err := writeTestVerificationRecord(newVerificationPath, verification.Repository{Owner: "owner", Name: "repo"}, previous.Package, newVersion); err != nil {
		return app.UpdateInstalledResult{}, err
	}
	nextBinaries := []app.InstalledBinary{
		{
			Name:       previous.Package,
			LinkPath:   filepath.Join(binRoot, previous.Package),
			TargetPath: newTargetPath,
		},
	}
	installer := filesystem.NewInstaller()
	canSwapManagedLinks := true
	for _, binary := range previousBinaries {
		if _, err := os.Lstat(binary.LinkPath); err != nil {
			canSwapManagedLinks = false
			break
		}
	}
	if canSwapManagedLinks {
		if err := installer.ReplaceManagedBinaries(ctx, app.ReplaceManagedBinariesRequest{
			BinDir:   binRoot,
			Previous: previousBinaries,
			Next:     nextBinaries,
		}); err != nil {
			return app.UpdateInstalledResult{}, err
		}
	}
	current := previous
	current.Version = newVersion
	current.Tag = "v" + newVersion
	current.StorePath = newStorePath
	current.ArtifactPath = newArtifactPath
	current.ExtractedPath = newExtractedPath
	current.VerificationPath = newVerificationPath
	current.AssetDigest = digest.String()
	current.Binaries = []state.Binary{
		{Name: previous.Package, LinkPath: nextBinaries[0].LinkPath, TargetPath: nextBinaries[0].TargetPath},
	}
	current.InstalledAt = time.Unix(1700000100, 0).UTC()
	if _, err := store.ReplaceInstalledRecord(ctx, request.StateDir, current); err != nil {
		return app.UpdateInstalledResult{}, err
	}
	row := app.UpdateInstalledResult{
		Repository:      previous.Repository,
		Package:         previous.Package,
		PreviousVersion: previous.Version,
		CurrentVersion:  current.Version,
		Status:          app.UpdateStatusUpdated,
	}
	if previous.Repository == "owner/warn" {
		reason := fmt.Sprintf("updated %s/%s@%s -> %s but failed to remove previous store: permission denied", previous.Repository, previous.Package, previous.Version, current.Version)
		row.Status = app.UpdateStatusUpdatedWithWarning
		row.Reason = reason
		return row, errors.New(reason)
	}
	if canSwapManagedLinks {
		if err := installer.RemoveManagedStore(ctx, storeRoot, previous.StorePath); err != nil {
			return app.UpdateInstalledResult{}, err
		}
	}
	return row, nil
}

func testRepositoryFromString(value string) (verification.Repository, error) {
	owner, name, ok := strings.Cut(value, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
		return verification.Repository{}, fmt.Errorf("repository must be owner/repo")
	}
	return verification.Repository{Owner: owner, Name: name}, nil
}

func (testRuntime) ListInstalled(ctx context.Context, stateDir string) ([]state.Record, error) {
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	return index.Normalize().Records, nil
}

func (testRuntime) VerifyInstalled(ctx context.Context, request app.VerifyInstalledRequest) ([]app.VerifyInstalledResult, error) {
	subject, err := app.NewInstalledPackageVerifier(app.InstalledPackageVerifierDependencies{
		StateStore:    filesystem.NewInstalledStore(),
		Verifier:      testReleaseVerifier{},
		EvidenceStore: filesystem.NewEvidenceWriter(),
		Archives:      testArchiveExtractor{},
		FileSystem:    filesystem.NewInstaller(),
	})
	if err != nil {
		return nil, err
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
	if request.Progress != nil {
		request.Progress(app.InstallProgress{Stage: app.InstallProgressCheckingState, Message: "Checking installed packages"})
	}
	store := filesystem.NewInstalledStore()
	index, err := store.LoadInstalledState(ctx, request.StateDir)
	if err != nil {
		return app.VerifiedInstallResult{}, err
	}
	if err := index.CheckBinaryOwnership(state.PackageRef{
		Repository: request.Repository.String(),
		Package:    request.PackageName,
	}, []string{request.PackageName}, state.PackageRef{}); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	digest, err := verification.NewDigest("sha256", strings.Repeat("a", 64))
	if err != nil {
		return app.VerifiedInstallResult{}, err
	}
	evidence := verification.Evidence{
		Repository:  request.Repository,
		Tag:         verification.ReleaseTag("v" + request.Version),
		AssetDigest: digest,
		ReleaseTagDigest: func() verification.Digest {
			releaseDigest, _ := verification.NewDigest("sha1", strings.Repeat("b", 40))
			return releaseDigest
		}(),
		ReleaseAttestation: verification.AttestationEvidence{
			AttestationID: "release",
			PredicateType: verification.ReleasePredicateV02,
		},
		ProvenanceAttestation: verification.AttestationEvidence{
			AttestationID:  "provenance",
			PredicateType:  verification.SLSAPredicateV1,
			SignerWorkflow: verification.WorkflowIdentity(request.Repository.String() + "/.github/workflows/release.yml"),
		},
	}
	if request.Progress != nil {
		request.Progress(app.InstallProgress{Stage: app.InstallProgressVerifying, Message: "Verifying release and provenance"})
	}
	if request.Approve != nil {
		if err := request.Approve(ctx, app.InstallApproval{
			Repository:              request.Repository,
			PackageName:             request.PackageName,
			Version:                 request.Version,
			Tag:                     evidence.Tag,
			AssetName:               request.PackageName + ".tar.gz",
			AssetDigest:             evidence.AssetDigest,
			ReleasePredicateType:    evidence.ReleaseAttestation.PredicateType,
			ProvenancePredicateType: evidence.ProvenanceAttestation.PredicateType,
			SignerWorkflow:          evidence.ProvenanceAttestation.SignerWorkflow,
			BinDir:                  request.BinDir,
			Binaries:                []string{request.PackageName},
		}); err != nil {
			return app.VerifiedInstallResult{}, err
		}
	}
	if request.Progress != nil {
		request.Progress(app.InstallProgress{Stage: app.InstallProgressPreparingStore, Message: "Preparing managed store"})
	}
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
		Tag:              string(evidence.Tag),
		Asset:            request.PackageName + ".tar.gz",
		AssetDigest:      evidence.AssetDigest.String(),
		StorePath:        storePath,
		ArtifactPath:     artifactPath,
		ExtractedPath:    extractedPath,
		VerificationPath: verificationPath,
		Binaries:         []state.Binary{{Name: request.PackageName, LinkPath: linkPath, TargetPath: linkTarget}},
		InstalledAt:      time.Unix(1700000000, 0).UTC(),
	}
	if _, err := store.AddInstalledRecord(ctx, request.StateDir, record); err != nil {
		return app.VerifiedInstallResult{}, err
	}
	return app.VerifiedInstallResult{
		Repository:  request.Repository,
		PackageName: request.PackageName,
		Version:     request.Version,
		Tag:         evidence.Tag,
		AssetName:   request.PackageName + ".tar.gz",
		Evidence:    evidence,
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

func (testRuntimeManifestSource) FetchManifestAtRef(ctx context.Context, repository verification.Repository, _ string) ([]byte, error) {
	return testRuntimeManifestSource{}.FetchManifest(ctx, repository)
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
