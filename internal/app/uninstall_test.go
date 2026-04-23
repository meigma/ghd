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

	result, err := tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	assert.Equal(t, "owner/repo", result.Record.Repository)
	assert.Equal(t, "foo", result.Record.Package)
	assert.Equal(t, []InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/foo/extracted/foo"}}, tc.files.removedLinks)
	assert.Equal(t, "owner/repo", tc.state.removedRepository)
	assert.Equal(t, "foo", tc.state.removedPackage)
	assert.Equal(t, result.Record.StorePath, tc.files.removedStore.StorePath)
	assert.Equal(t, []string{"state-load", "validate-store", "remove-links", "remove-store", "state-remove"}, tc.events)
}

func TestPackageUninstallerRejectsAmbiguousTargetsBeforeRemovingLinks(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/one", "foo"))
	require.NoError(t, err)
	tc.state.index, err = tc.state.index.AddRecord(withInstalledBinaries(installedRecord("owner/two", "bar"), []state.Binary{
		{Name: "foo", LinkPath: "/bin/foo-two", TargetPath: "/store/bar/extracted/foo"},
	}))
	require.NoError(t, err)

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
	assert.Empty(t, tc.files.removedLinks)
	assert.Empty(t, tc.state.removedRepository)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestPackageUninstallerDoesNotRemoveLinksWhenStoreValidationFails(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.files.validateErr = errors.New("store path is not under store root")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "store path is not under store root")
	assert.Empty(t, tc.files.removedLinks)
	assert.Empty(t, tc.state.removedRepository)
	assert.Equal(t, []string{"state-load", "validate-store"}, tc.events)
}

func TestPackageUninstallerDoesNotRemoveStateWhenLinkCleanupFails(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.files.linkErr = errors.New("unexpected link target")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "owner/repo/foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected link target")
	assert.Empty(t, tc.state.removedRepository)
	assert.Empty(t, tc.files.removedStore.StorePath)
	assert.Equal(t, []string{"state-load", "validate-store", "remove-links"}, tc.events)
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
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write installed state")
	assert.Equal(t, "/store/foo", tc.files.removedStore.StorePath)
	assert.Equal(t, "owner/repo", tc.state.removedRepository)
	assert.Equal(t, []string{"state-load", "validate-store", "remove-links", "remove-store", "state-remove"}, tc.events)
}

func TestPackageUninstallerKeepsStateWhenStoreCleanupFails(t *testing.T) {
	tc := newUninstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.files.storeErr = errors.New("permission denied")

	_, err = tc.subject.Uninstall(context.Background(), UninstallRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Empty(t, tc.state.removedRepository)
	assert.Equal(t, []string{"state-load", "validate-store", "remove-links", "remove-store"}, tc.events)
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

func (f *fakeUninstallStateStore) RemoveInstalledRecord(_ context.Context, _ string, repository string, packageName string) (state.Index, error) {
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
	events       *[]string
	removedLinks []InstalledBinary
	removedStore RemoveInstalledStoreRequest
	validateErr  error
	linkErr      error
	storeErr     error
}

func (f *fakeUninstallFileSystem) ValidateInstalledStore(_ context.Context, _ RemoveInstalledStoreRequest) error {
	*f.events = append(*f.events, "validate-store")
	return f.validateErr
}

func (f *fakeUninstallFileSystem) RemoveBinaryLinks(_ context.Context, binaries []InstalledBinary) error {
	*f.events = append(*f.events, "remove-links")
	f.removedLinks = append(f.removedLinks, binaries...)
	return f.linkErr
}

func (f *fakeUninstallFileSystem) RemoveInstalledStore(_ context.Context, request RemoveInstalledStoreRequest) error {
	*f.events = append(*f.events, "remove-store")
	f.removedStore = request
	return f.storeErr
}

func withInstalledBinaries(record state.Record, binaries []state.Binary) state.Record {
	record.Binaries = binaries
	return record
}
