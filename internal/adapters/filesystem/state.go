package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	lockPath := filepath.Join(stateDir, installedStateLockFile)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open installed state lock: %w", err)
	}
	for {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			unlock := func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}
			return unlock, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock installed state: %w", err)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
