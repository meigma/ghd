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
	index = index.Normalize()
	if err := index.Validate(); err != nil {
		return state.Index{}, err
	}
	return index, nil
}

// SaveInstalledState writes active installed package state to stateDir.
func (InstalledStore) SaveInstalledState(ctx context.Context, stateDir string, index state.Index) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state directory must be set")
	}
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
