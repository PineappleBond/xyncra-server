package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestDefaultDeviceID_Deterministic(t *testing.T) {
	// Swap osHostname for a fixed value.
	orig := osHostname
	osHostname = func() (string, error) { return "test-host", nil }
	defer func() { osHostname = orig }()

	a := defaultDeviceID()
	b := defaultDeviceID()
	if a != b {
		t.Errorf("defaultDeviceID() not deterministic: %q vs %q", a, b)
	}
}

func TestDefaultDeviceID_Format(t *testing.T) {
	orig := osHostname
	osHostname = func() (string, error) { return "test-host", nil }
	defer func() { osHostname = orig }()

	id := defaultDeviceID()
	matched, err := regexp.MatchString(`^[0-9a-f]{8}$`, id)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("defaultDeviceID() = %q, want 8 hex chars matching ^[0-9a-f]{8}$", id)
	}
}

func TestDefaultDeviceID_DifferentHostname(t *testing.T) {
	orig := osHostname
	defer func() { osHostname = orig }()

	osHostname = func() (string, error) { return "hostA", nil }
	a := defaultDeviceID()

	osHostname = func() (string, error) { return "hostB", nil }
	b := defaultDeviceID()

	if a == b {
		t.Errorf("different hostnames produced same device ID: %q", a)
	}
}

func TestDefaultDeviceID_HostnameError(t *testing.T) {
	orig := osHostname
	osHostname = func() (string, error) { return "", os.ErrNotExist }
	defer func() { osHostname = orig }()

	// Should not panic; falls back to "unknown"
	id := defaultDeviceID()
	if id == "" {
		t.Error("defaultDeviceID() returned empty string on hostname error")
	}
	matched, _ := regexp.MatchString(`^[0-9a-f]{8}$`, id)
	if !matched {
		t.Errorf("defaultDeviceID() on error = %q, want 8 hex chars", id)
	}
}

func TestEnsureUserDir_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir, err := ensureUserDir("user1", "dev1")
	if err != nil {
		t.Fatalf("ensureUserDir() error: %v", err)
	}

	expected := filepath.Join(tmpDir, ".xyncra", "user1", "dev1")
	if dir != expected {
		t.Errorf("ensureUserDir() = %q, want %q", dir, expected)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("ensureUserDir() created path is not a directory")
	}
}

func TestEnsureUserDir_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err1 := ensureUserDir("user1", "dev1")
	_, err2 := ensureUserDir("user1", "dev1")
	_, err3 := ensureUserDir("user1", "dev1")

	if err1 != nil || err2 != nil || err3 != nil {
		t.Errorf("ensureUserDir() not idempotent: errors %v, %v, %v", err1, err2, err3)
	}
}

func TestCLIContext_SocketPath(t *testing.T) {
	ctx := &CLIContext{UserDir: "/tmp/test-user-dir"}
	got := ctx.SocketPath()
	want := filepath.Join("/tmp/test-user-dir", "xyncra.sock")
	if got != want {
		t.Errorf("SocketPath() = %q, want %q", got, want)
	}
}

func TestCLIContext_LockPath(t *testing.T) {
	ctx := &CLIContext{UserDir: "/tmp/test-user-dir"}
	got := ctx.LockPath()
	want := filepath.Join("/tmp/test-user-dir", "xyncra.lock")
	if got != want {
		t.Errorf("LockPath() = %q, want %q", got, want)
	}
}

func TestCLIContext_DBPathDefault(t *testing.T) {
	ctx := &CLIContext{UserDir: "/tmp/test-user-dir"}
	got := ctx.DBPathDefault()
	want := filepath.Join("/tmp/test-user-dir", "xyncra.db")
	if got != want {
		t.Errorf("DBPathDefault() = %q, want %q", got, want)
	}
}

func TestCLIContext_LogDirDefault(t *testing.T) {
	ctx := &CLIContext{UserDir: "/tmp/test-user-dir"}
	got := ctx.LogDirDefault()
	want := filepath.Join("/tmp/test-user-dir", "logs") + string(filepath.Separator)
	if got != want {
		t.Errorf("LogDirDefault() = %q, want %q", got, want)
	}
}
