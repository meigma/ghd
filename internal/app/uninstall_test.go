package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/state"
)

func TestPackageUninstallerRemovesLinksStateAndStore(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	result, err := tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: storeDir,
		BinDir:   binDir,
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	assert.Equal(t, "owner/repo", result.Repository)
	assert.Equal(t, "foo", result.Package)
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, storeDir, tc.files.removedManaged.StoreRoot)
	assert.Equal(t, binDir, tc.files.removedManaged.BinRoot)
	assert.Equal(
		t,
		[]InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"}},
		tc.files.removedManaged.Binaries,
	)
	assert.Equal(t, "owner/repo", tc.state.removedRepository)
	assert.Equal(t, "foo", tc.state.removedPackage)
	assert.Equal(t, result.StorePath, tc.files.removedManaged.StorePath)
	assert.Equal(t, []string{"state-load", "remove-managed", "state-remove"}, tc.events)
}

func TestPackageUninstallerReportsProgressInOrder(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)

	var stages []UninstallProgressStage
	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
		Progress: func(progress UninstallProgress) {
			stages = append(stages, progress.Stage)
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []UninstallProgressStage{
		UninstallProgressLoadingState,
		UninstallProgressRemovingManaged,
		UninstallProgressRemovingState,
	}, stages)
}

func TestPackageUninstallerRejectsAmbiguousTargetsBeforeRemovingLinks(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(
		withInstalledBinaries(installedRecord("owner/one", "foo"), []state.Binary{
			{Name: "one", LinkPath: "/bin/one", TargetPath: "/store/foo/extracted/one"},
		}),
	)
	require.NoError(t, err)
	tc.state.index, err = tc.state.index.AddRecord(
		withInstalledBinaries(installedRecord("owner/two", "bar"), []state.Binary{
			{Name: "foo", LinkPath: "/bin/foo-two", TargetPath: "/store/bar/extracted/foo"},
		}),
	)
	require.NoError(t, err)

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
	assert.Nil(t, tc.files.removedManaged)
	assert.Empty(t, tc.state.removedRepository)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestPackageUninstallerDoesNotRemoveLinksWhenStoreValidationFails(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.files.removeErr = errors.New("store path is not under store root")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "store path is not under store root")
	assert.Nil(t, tc.files.removedManaged)
	assert.Empty(t, tc.state.removedRepository)
	assert.Equal(t, []string{"state-load", "remove-managed"}, tc.events)
}

func TestPackageUninstallerDoesNotRemoveStateWhenLinkCleanupFails(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.files.removeErr = errors.New("unexpected link target")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "owner/repo/foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected link target")
	assert.Empty(t, tc.state.removedRepository)
	assert.Nil(t, tc.files.removedManaged)
	assert.Equal(t, []string{"state-load", "remove-managed"}, tc.events)
}

func TestPackageUninstallerReportsStateRemovalFailureAfterStoreCleanup(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.state.removeErr = errors.New("write installed state")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write installed state")
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, "/store/foo", tc.files.removedManaged.StorePath)
	assert.Equal(t, "owner/repo", tc.state.removedRepository)
	assert.Equal(t, []string{"state-load", "remove-managed", "state-remove"}, tc.events)
}

func TestPackageUninstallerKeepsStateWhenStoreCleanupFails(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.files.removeErr = errors.New("permission denied")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Empty(t, tc.state.removedRepository)
	assert.Equal(t, []string{"state-load", "remove-managed"}, tc.events)
}

type uninstallTestContext struct {
	state   *fakeUninstallStateStore
	files   *fakeUninstallFileSystem
	events  []string
	subject *PackageUninstaller
}

func newUninstallTestContext(t *testing.T) *uninstallTestContext {
	t.Helper()
	tc := &uninstallTestContext{
		state: &fakeUninstallStateStore{index: state.NewIndex()},
	}
	tc.state.events = &tc.events
	tc.files = &fakeUninstallFileSystem{events: &tc.events}
	subject, err := NewPackageUninstaller(PackageUninstallerDependencies{
		StateStore: tc.state,
		FileSystem: tc.files,
	})
	require.NoError(t, err)
	tc.subject = subject
	return tc
}

type fakeUninstallStateStore struct {
	events            *[]string
	index             state.Index
	removedRepository string
	removedPackage    string
	loadErr           error
	removeErr         error
}

func (f *fakeUninstallStateStore) LoadInstalledState(context.Context, string) (state.Index, error) {
	*f.events = append(*f.events, "state-load")
	if f.loadErr != nil {
		return state.Index{}, f.loadErr
	}
	return f.index.Normalize(), nil
}

func (f *fakeUninstallStateStore) RemoveInstalledRecord(
	_ context.Context,
	_ string,
	repository string,
	packageName string,
) (state.Index, error) {
	*f.events = append(*f.events, "state-remove")
	f.removedRepository = repository
	f.removedPackage = packageName
	if f.removeErr != nil {
		return state.Index{}, f.removeErr
	}
	index, _, err := f.index.RemoveRecord(repository, packageName)
	if err != nil {
		return state.Index{}, err
	}
	f.index = index
	return f.index, nil
}

type fakeUninstallFileSystem struct {
	events         *[]string
	removedManaged *RemoveManagedInstallRequest
	removeErr      error
}

func (f *fakeUninstallFileSystem) RemoveManagedInstall(_ context.Context, request RemoveManagedInstallRequest) error {
	*f.events = append(*f.events, "remove-managed")
	if f.removeErr != nil {
		return f.removeErr
	}
	copied := request
	copied.Binaries = append([]InstalledBinary(nil), request.Binaries...)
	f.removedManaged = &copied
	return nil
}

func withInstalledBinaries(record state.Record, binaries []state.Binary) state.Record {
	record.Binaries = binaries
	return record
}
