package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockRetryDelay = 50 * time.Millisecond

func acquireFileLock(
	ctx context.Context,
	dir string,
	lockFile string,
	dirLabel string,
	lockLabel string,
) (func(), error) {
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return nil, fmt.Errorf("create %s directory: %w", dirLabel, err)
	}
	lockPath := filepath.Join(dir, lockFile)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, privateFileMode)
	if err != nil {
		return nil, fmt.Errorf("open %s lock: %w", lockLabel, err)
	}
	for {
		if err := flock(file, syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			unlock := func() {
				_ = flock(file, syscall.LOCK_UN)
				_ = file.Close()
			}
			return unlock, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock %s: %w", lockLabel, err)
		}
		timer := time.NewTimer(lockRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func flock(file *os.File, operation int) error {
	//nolint:gosec // syscall.Flock requires an int file descriptor.
	return syscall.Flock(int(file.Fd()), operation)
}
