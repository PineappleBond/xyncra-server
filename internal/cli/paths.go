package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// osHostname is a variable holding os.Hostname so it can be swapped in tests.
var osHostname = os.Hostname

// defaultDeviceID returns the hostname's SHA256 hash first 8 hex characters (D-033).
// The result is deterministic, anonymous, and requires zero configuration.
func defaultDeviceID() string {
	hostname, err := osHostname()
	if err != nil {
		hostname = "unknown"
	}
	h := sha256.Sum256([]byte(hostname))
	return fmt.Sprintf("%x", h[:4])
}

// ensureUserDir creates ~/.xyncra/{user_id}/{device_id}/ and returns the path.
func ensureUserDir(userID, deviceID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ensureUserDir: %w", err)
	}
	dir := filepath.Join(home, ".xyncra", userID, deviceID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("ensureUserDir: %w", err)
	}
	return dir, nil
}

// SocketPath returns the path to the Unix domain socket for the daemon.
func (c *CLIContext) SocketPath() string {
	return filepath.Join(c.UserDir, "xyncra.sock")
}

// LockPath returns the path to the lock file for the daemon.
func (c *CLIContext) LockPath() string {
	return filepath.Join(c.UserDir, "xyncra.lock")
}

// DBPathDefault returns the default path to the SQLite database.
func (c *CLIContext) DBPathDefault() string {
	return filepath.Join(c.UserDir, "xyncra.db")
}

// LogDirDefault returns the default directory for log files.
func (c *CLIContext) LogDirDefault() string {
	return filepath.Join(c.UserDir, "logs") + string(filepath.Separator)
}

// ServerURLWithUser returns the server URL with user_id query parameter appended.
func (c *CLIContext) ServerURLWithUser() string {
	return c.ServerURL + "?user_id=" + c.UserID
}
