package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestCommand creates a cobra.Command with the same persistent flags as
// NewRootCommand, for testing purposes.
func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.PersistentFlags().StringP("user-id", "u", "", "User ID")
	cmd.PersistentFlags().String("device-id", "", "Device ID")
	cmd.PersistentFlags().StringP("server", "s", "", "Server URL")
	cmd.PersistentFlags().String("db-path", "", "Database path")
	cmd.PersistentFlags().String("log-dir", "", "Log directory")
	return cmd
}

// setFlag sets a persistent flag value and marks it as changed, simulating
// command-line parsing. It merges persistent flags into cmd.Flags() first,
// because resolveStringFlag reads from cmd.Flags().
func setFlag(cmd *cobra.Command, name, value string) {
	// Merge persistent flags into Flags() (simulates cobra's execution behavior).
	cmd.Flags().AddFlagSet(cmd.PersistentFlags())
	// Set the value on the merged FlagSet.
	_ = cmd.Flags().Set(name, value)
	if f := cmd.Flags().Lookup(name); f != nil {
		f.Changed = true
	}
}

func TestResolveStringFlag_FlagOverridesEnvAndDefault(t *testing.T) {
	cmd := newTestCommand()
	setFlag(cmd, "user-id", "flag-user")

	t.Setenv("XYNCRA_USER_ID", "env-user")

	got := resolveStringFlag(cmd, "user-id", "XYNCRA_USER_ID", "default-user")
	if got != "flag-user" {
		t.Errorf("resolveStringFlag() = %q, want %q (flag should win)", got, "flag-user")
	}
}

func TestResolveStringFlag_EnvOverridesDefault(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv("XYNCRA_USER_ID", "env-user")

	got := resolveStringFlag(cmd, "user-id", "XYNCRA_USER_ID", "default-user")
	if got != "env-user" {
		t.Errorf("resolveStringFlag() = %q, want %q (env should win)", got, "env-user")
	}
}

func TestResolveStringFlag_DefaultUsed(t *testing.T) {
	cmd := newTestCommand()
	// No flag set, no env var.
	t.Setenv("XYNCRA_USER_ID", "")

	got := resolveStringFlag(cmd, "user-id", "XYNCRA_USER_ID", "default-user")
	if got != "default-user" {
		t.Errorf("resolveStringFlag() = %q, want %q (default should win)", got, "default-user")
	}
}

func TestNewCLIContext_MissingUserID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")

	cmd := newTestCommand()
	// Don't set user-id flag or env var.
	_, err := NewCLIContext(cmd)
	if err == nil {
		t.Fatal("NewCLIContext() should fail when user-id is missing")
	}
	if !strings.Contains(err.Error(), "user-id is required") {
		t.Errorf("error = %q, want it to contain 'user-id is required'", err.Error())
	}
}

func TestNewCLIContext_FromFlags(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	// Clear env vars to avoid interference.
	t.Setenv("XYNCRA_USER_ID", "")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	cmd := newTestCommand()
	setFlag(cmd, "user-id", "testuser")
	setFlag(cmd, "device-id", "testdevice")
	setFlag(cmd, "server", "ws://example.com/ws")

	ctx, err := NewCLIContext(cmd)
	if err != nil {
		t.Fatalf("NewCLIContext() error: %v", err)
	}
	if ctx.UserID != "testuser" {
		t.Errorf("UserID = %q, want %q", ctx.UserID, "testuser")
	}
	if ctx.DeviceID != "testdevice" {
		t.Errorf("DeviceID = %q, want %q", ctx.DeviceID, "testdevice")
	}
	if ctx.ServerURL != "ws://example.com/ws" {
		t.Errorf("ServerURL = %q, want %q", ctx.ServerURL, "ws://example.com/ws")
	}
}

func TestNewCLIContext_FromEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "envuser")
	t.Setenv("XYNCRA_DEVICE_ID", "envdevice")
	t.Setenv("XYNCRA_SERVER", "ws://env.example.com/ws")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	cmd := newTestCommand()

	ctx, err := NewCLIContext(cmd)
	if err != nil {
		t.Fatalf("NewCLIContext() error: %v", err)
	}
	if ctx.UserID != "envuser" {
		t.Errorf("UserID = %q, want %q", ctx.UserID, "envuser")
	}
	if ctx.DeviceID != "envdevice" {
		t.Errorf("DeviceID = %q, want %q", ctx.DeviceID, "envdevice")
	}
	if ctx.ServerURL != "ws://env.example.com/ws" {
		t.Errorf("ServerURL = %q, want %q", ctx.ServerURL, "ws://env.example.com/ws")
	}
}

func TestNewCLIContext_DefaultPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "testuser")
	t.Setenv("XYNCRA_DEVICE_ID", "testdevice")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	cmd := newTestCommand()

	ctx, err := NewCLIContext(cmd)
	if err != nil {
		t.Fatalf("NewCLIContext() error: %v", err)
	}

	expectedUserDir := filepath.Join(tmpDir, ".xyncra", "testuser", "testdevice")
	if ctx.UserDir != expectedUserDir {
		t.Errorf("UserDir = %q, want %q", ctx.UserDir, expectedUserDir)
	}

	expectedDB := filepath.Join(expectedUserDir, "xyncra.db")
	if ctx.DBPath != expectedDB {
		t.Errorf("DBPath = %q, want %q", ctx.DBPath, expectedDB)
	}

	expectedLogDir := filepath.Join(expectedUserDir, "logs") + string(filepath.Separator)
	if ctx.LogDir != expectedLogDir {
		t.Errorf("LogDir = %q, want %q", ctx.LogDir, expectedLogDir)
	}
}

func TestNewCLIContext_FlagOverridesEnv(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XYNCRA_USER_ID", "envuser")
	t.Setenv("XYNCRA_DEVICE_ID", "")
	t.Setenv("XYNCRA_SERVER", "")
	t.Setenv("XYNCRA_DB_PATH", "")
	t.Setenv("XYNCRA_LOG_DIR", "")

	cmd := newTestCommand()
	setFlag(cmd, "user-id", "flaguser")

	ctx, err := NewCLIContext(cmd)
	if err != nil {
		t.Fatalf("NewCLIContext() error: %v", err)
	}
	// Flag should override env.
	if ctx.UserID != "flaguser" {
		t.Errorf("UserID = %q, want %q (flag should override env)", ctx.UserID, "flaguser")
	}
}

func TestNewRootCommand(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.Use != "xyncra-client" {
		t.Errorf("Use = %q, want %q", cmd.Use, "xyncra-client")
	}

	// Check subcommands exist.
	subCmds := cmd.Commands()
	names := make(map[string]bool)
	for _, sc := range subCmds {
		names[sc.Use] = true
	}
	if !names["listen"] {
		t.Error("missing 'listen' subcommand")
	}
	if !names["send"] {
		t.Error("missing 'send' subcommand")
	}

	// Phase 2 commands.
	expectedCmds := []string{
		"create-conversation",
		"delete-conversation",
		"restore-conversation",
		"list-conversations",
		"get-conversation",
		"delete-message",
		"mark-as-read",
		"get-messages",
		"search-messages",
		"draft",
		"sync-updates",
	}
	for _, name := range expectedCmds {
		if !names[name] {
			t.Errorf("missing %q subcommand", name)
		}
	}
}

func TestNewRootCommand_TotalSubcommandCount(t *testing.T) {
	cmd := NewRootCommand()
	// 13 subcommands: listen, send, create-conversation, delete-conversation,
	// restore-conversation, list-conversations, get-conversation, delete-message,
	// mark-as-read, get-messages, search-messages, draft, sync-updates.
	if got := len(cmd.Commands()); got != 13 {
		t.Errorf("expected 13 subcommands, got %d", got)
	}
}

func TestNewRootCommand_HelpTexts(t *testing.T) {
	cmd := NewRootCommand()
	for _, sub := range cmd.Commands() {
		if sub.Short == "" {
			t.Errorf("subcommand %q has empty Short help text", sub.Use)
		}
	}
}
