package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireLock_Success(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	info := &LockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		DeviceID:  "test-device",
	}

	unlock, err := acquireLock(lockPath, info)
	if err != nil {
		t.Fatalf("acquireLock() error: %v", err)
	}
	defer func() { _ = unlock() }()

	// Lock file should exist.
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should exist: %v", err)
	}

	// Read lock info and verify.
	readInfo, err := readLockInfo(lockPath)
	if err != nil {
		t.Fatalf("readLockInfo() error: %v", err)
	}
	if readInfo.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", readInfo.PID, os.Getpid())
	}
	if readInfo.DeviceID != "test-device" {
		t.Errorf("DeviceID = %q, want %q", readInfo.DeviceID, "test-device")
	}
}

func TestAcquireLock_DuplicateFails(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	info := &LockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		DeviceID:  "test-device",
	}

	unlock, err := acquireLock(lockPath, info)
	if err != nil {
		t.Fatalf("first acquireLock() error: %v", err)
	}
	defer func() { _ = unlock() }()

	// Second acquire should fail because the lock is held.
	_, err = acquireLock(lockPath, info)
	if err == nil {
		t.Fatal("second acquireLock() should fail, but succeeded")
	}
}

func TestAcquireLock_ReleaseAndReacquire(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	info := &LockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		DeviceID:  "test-device",
	}

	unlock, err := acquireLock(lockPath, info)
	if err != nil {
		t.Fatalf("first acquireLock() error: %v", err)
	}

	// Release the lock.
	if err := unlock(); err != nil {
		t.Fatalf("unlock() error: %v", err)
	}

	// Should be able to acquire again.
	unlock2, err := acquireLock(lockPath, info)
	if err != nil {
		t.Fatalf("second acquireLock() error: %v", err)
	}
	defer func() { _ = unlock2() }()
}

func TestAcquireLock_StaleLockCleanup(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	// Write a lock file with a non-existent PID (stale lock).
	staleInfo := &LockInfo{
		PID:       99999999, // Very unlikely to be a real PID.
		StartedAt: time.Now().Add(-1 * time.Hour),
		DeviceID:  "stale-device",
	}
	data, err := json.Marshal(staleInfo)
	if err != nil {
		t.Fatalf("marshal stale info: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0600); err != nil {
		t.Fatalf("write stale lock file: %v", err)
	}

	// acquireLock should detect the stale lock and succeed.
	info := &LockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		DeviceID:  "test-device",
	}

	unlock, err := acquireLock(lockPath, info)
	if err != nil {
		t.Fatalf("acquireLock() should clean up stale lock, got error: %v", err)
	}
	defer func() { _ = unlock() }()

	// Verify new lock info was written.
	readInfo, err := readLockInfo(lockPath)
	if err != nil {
		t.Fatalf("readLockInfo() error: %v", err)
	}
	if readInfo.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", readInfo.PID, os.Getpid())
	}
	if readInfo.DeviceID != "test-device" {
		t.Errorf("DeviceID = %q, want %q", readInfo.DeviceID, "test-device")
	}
}

func TestAcquireLock_ActivePIDNotCleaned(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	// First, acquire the lock legitimately.
	info := &LockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		DeviceID:  "first-device",
	}

	unlock, err := acquireLock(lockPath, info)
	if err != nil {
		t.Fatalf("first acquireLock() error: %v", err)
	}
	defer func() { _ = unlock() }()

	// Second acquire should fail because the current process is alive.
	// The lock file has current PID, and isProcessAlive will return true.
	info2 := &LockInfo{
		PID:       os.Getpid() + 1000, // Different PID, but the lock file has our PID.
		StartedAt: time.Now(),
		DeviceID:  "second-device",
	}

	_, err = acquireLock(lockPath, info2)
	if err == nil {
		t.Fatal("acquireLock() should fail when lock is held by active process")
	}
}

func TestIsProcessAlive(t *testing.T) {
	t.Run("current process", func(t *testing.T) {
		if !isProcessAlive(os.Getpid()) {
			t.Error("isProcessAlive(current PID) = false, want true")
		}
	})

	t.Run("non-existent PID", func(t *testing.T) {
		if isProcessAlive(99999999) {
			t.Error("isProcessAlive(99999999) = true, want false")
		}
	})

	t.Run("invalid PID", func(t *testing.T) {
		if isProcessAlive(0) {
			t.Error("isProcessAlive(0) = true, want false")
		}
		if isProcessAlive(-1) {
			t.Error("isProcessAlive(-1) = true, want false")
		}
	})
}

func TestReadWriteLockInfo(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	now := time.Now().Truncate(time.Millisecond)
	info := &LockInfo{
		PID:       12345,
		StartedAt: now,
		DeviceID:  "my-device",
	}

	if err := writeLockInfo(lockPath, info); err != nil {
		t.Fatalf("writeLockInfo() error: %v", err)
	}

	read, err := readLockInfo(lockPath)
	if err != nil {
		t.Fatalf("readLockInfo() error: %v", err)
	}

	if read.PID != info.PID {
		t.Errorf("PID = %d, want %d", read.PID, info.PID)
	}
	if read.DeviceID != info.DeviceID {
		t.Errorf("DeviceID = %q, want %q", read.DeviceID, info.DeviceID)
	}
}
