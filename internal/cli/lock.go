package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/gofrs/flock"
)

// LockInfo contains metadata about the process holding the daemon lock.
type LockInfo struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	DeviceID  string    `json:"device_id"`
}

// acquireLock attempts to obtain an exclusive flock on lockPath.
// If the lock is held by a live process, returns an error.
// If the lock is stale (holder process dead), cleans up and retries.
func acquireLock(lockPath string, info *LockInfo) (unlockFn func() error, err error) {
	f := flock.New(lockPath)
	locked, err := f.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire lock trylock: %w", err)
	}

	if locked {
		// Lock acquired — write LockInfo into the lock file.
		if err := writeLockInfo(lockPath, info); err != nil {
			_ = f.Unlock()
			return nil, fmt.Errorf("acquire lock write info: %w", err)
		}
		return makeUnlockFn(f, lockPath), nil
	}

	// Lock is held by another process. Check if it is stale.
	existing, err := readLockInfo(lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire lock read existing info: %w", err)
	}

	if isProcessAlive(existing.PID) {
		return nil, fmt.Errorf("listen already running (PID: %d)", existing.PID)
	}

	// Stale lock — clean up and retry once.
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("acquire lock remove stale: %w", err)
	}

	// Retry after removing stale lock.
	f = flock.New(lockPath)
	locked, err = f.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire lock retry trylock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("acquire lock retry: lock still held after stale cleanup")
	}

	if err := writeLockInfo(lockPath, info); err != nil {
		_ = f.Unlock()
		return nil, fmt.Errorf("acquire lock retry write info: %w", err)
	}
	return makeUnlockFn(f, lockPath), nil
}

// makeUnlockFn returns a cleanup function that releases the flock and removes
// the lock file. Both operations are attempted even if one fails.
func makeUnlockFn(f *flock.Flock, lockPath string) func() error {
	return func() error {
		unlockErr := f.Unlock()
		removeErr := os.Remove(lockPath)
		if unlockErr != nil {
			return fmt.Errorf("unlock flock: %w", unlockErr)
		}
		if removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("remove lock file: %w", removeErr)
		}
		return nil
	}
}

// writeLockInfo serializes info as JSON and writes it to the lock file.
func writeLockInfo(lockPath string, info *LockInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("write lock info marshal: %w", err)
	}
	if err := os.WriteFile(lockPath, data, 0600); err != nil {
		return fmt.Errorf("write lock info write file: %w", err)
	}
	return nil
}

// readLockInfo reads and deserializes LockInfo from the lock file.
func readLockInfo(lockPath string) (*LockInfo, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("read lock info read file: %w", err)
	}
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("read lock info unmarshal: %w", err)
	}
	return &info, nil
}

// isProcessAlive checks if a process with the given PID exists.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, sending signal 0 checks process existence without killing it.
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
