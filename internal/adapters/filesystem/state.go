package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/state"
)

const installedStateFile = "installed.json"
const installedStateLockFile = ".installed.lock"

// InstalledStore persists active installed package state as JSON.
type InstalledStore struct{}

// NewInstalledStore creates a filesystem installed-state store.
func NewInstalledStore() InstalledStore {
	return InstalledStore{}
}

// LoadInstalledState reads active installed package state from stateDir.
func (InstalledStore) LoadInstalledState(ctx context.Context, stateDir string) (state.Index, error) {
	if err := ctx.Err(); err != nil {
		return state.Index{}, err
	}
	if strings.TrimSpace(stateDir) == "" {
		return state.Index{}, fmt.Errorf("state directory must be set")
	}
	return loadInstalledStateFile(stateDir)
}

// AddInstalledRecord adds an active installed package record under a state lock.
func (InstalledStore) AddInstalledRecord(ctx context.Context, stateDir string, record state.Record) (state.Index, error) {
	if err := ctx.Err(); err != nil {
		return state.Index{}, err
	}
	if strings.TrimSpace(stateDir) == "" {
		return state.Index{}, fmt.Errorf("state directory must be set")
	}
	unlock, err := acquireInstalledStateLock(ctx, stateDir)
	if err != nil {
		return state.Index{}, err
	}
	defer unlock()

	index, err := loadInstalledStateFile(stateDir)
	if err != nil {
		return state.Index{}, err
	}
	index, err = index.AddRecord(record)
	if err != nil {
		return state.Index{}, err
	}
	if err := saveInstalledStateFile(stateDir, index); err != nil {
		return state.Index{}, err
	}
	return index.Normalize(), nil
}

// RemoveInstalledRecord removes an active installed package record under a state lock.
func (InstalledStore) RemoveInstalledRecord(ctx context.Context, stateDir string, repository string, packageName string) (state.Index, error) {
	if err := ctx.Err(); err != nil {
		return state.Index{}, err
	}
	if strings.TrimSpace(stateDir) == "" {
		return state.Index{}, fmt.Errorf("state directory must be set")
	}
	unlock, err := acquireInstalledStateLock(ctx, stateDir)
	if err != nil {
		return state.Index{}, err
	}
	defer unlock()

	index, err := loadInstalledStateFile(stateDir)
	if err != nil {
		return state.Index{}, err
	}
	index, _, err = index.RemoveRecord(repository, packageName)
	if err != nil {
		return state.Index{}, err
	}
	if err := saveInstalledStateFile(stateDir, index); err != nil {
		return state.Index{}, err
	}
	return index.Normalize(), nil
}

// ReplaceInstalledRecord replaces one active installed package record under a state lock.
func (InstalledStore) ReplaceInstalledRecord(ctx context.Context, stateDir string, record state.Record) (state.Index, error) {
	if err := ctx.Err(); err != nil {
		return state.Index{}, err
	}
	if strings.TrimSpace(stateDir) == "" {
		return state.Index{}, fmt.Errorf("state directory must be set")
	}
	unlock, err := acquireInstalledStateLock(ctx, stateDir)
	if err != nil {
		return state.Index{}, err
	}
	defer unlock()

	index, err := loadInstalledStateFile(stateDir)
	if err != nil {
		return state.Index{}, err
	}
	index, err = index.ReplaceRecord(record)
	if err != nil {
		return state.Index{}, err
	}
	if err := saveInstalledStateFile(stateDir, index); err != nil {
		return state.Index{}, err
	}
	return index.Normalize(), nil
}

func loadInstalledStateFile(stateDir string) (state.Index, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, installedStateFile))
	if os.IsNotExist(err) {
		return state.NewIndex(), nil
	}
	if err != nil {
		return state.Index{}, fmt.Errorf("read installed state: %w", err)
	}
	var index state.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return state.Index{}, fmt.Errorf("decode installed state: %w", err)
	}
	if err := index.Validate(); err != nil {
		return state.Index{}, err
	}
	return index.Normalize(), nil
}

func saveInstalledStateFile(stateDir string, index state.Index) error {
	index = index.Normalize()
	if err := index.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode installed state: %w", err)
	}
	data = append(data, '\n')
	_, err = writeFileAtomic(stateDir, installedStateFile, data, 0o644)
	return err
}

func acquireInstalledStateLock(ctx context.Context, stateDir string) (func(), error) {
	return acquireFileLock(ctx, stateDir, installedStateLockFile, "state", "installed state")
}
