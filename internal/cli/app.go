package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// CLIContext holds resolved configuration for the CLI.
// Priority for resolving values: flag > env var > default (D-034).
type CLIContext struct {
	UserID    string
	DeviceID  string
	ServerURL string
	DBPath    string
	LogDir    string
	UserDir   string // ~/.xyncra/{user_id}/{device_id}
}

// resolveStringFlag resolves a flag value with priority: flag > env var > default.
func resolveStringFlag(cmd *cobra.Command, flagName, envVar, defaultVal string) string {
	if cmd.Flags().Changed(flagName) {
		val, _ := cmd.Flags().GetString(flagName)
		return val
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return defaultVal
}

// NewCLIContext resolves CLI context from flags and environment variables.
// Priority: flag > env var > default (D-034).
func NewCLIContext(cmd *cobra.Command) (*CLIContext, error) {
	userID := resolveStringFlag(cmd, "user-id", "XYNCRA_USER_ID", "")
	if userID == "" {
		return nil, fmt.Errorf("context: user-id is required (set via --user-id flag or XYNCRA_USER_ID env var)")
	}

	deviceID := resolveStringFlag(cmd, "device-id", "XYNCRA_DEVICE_ID", defaultDeviceID())
	serverURL := resolveStringFlag(cmd, "server", "XYNCRA_SERVER", "ws://localhost:8080/ws")

	userDir, err := ensureUserDir(userID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}

	// Build a partial CLIContext so dynamic defaults (which depend on UserDir) can be computed.
	ctx := &CLIContext{
		UserID:    userID,
		DeviceID:  deviceID,
		ServerURL: serverURL,
		UserDir:   userDir,
	}

	ctx.DBPath = resolveStringFlag(cmd, "db-path", "XYNCRA_DB_PATH", ctx.DBPathDefault())
	ctx.LogDir = resolveStringFlag(cmd, "log-dir", "XYNCRA_LOG_DIR", ctx.LogDirDefault())

	return ctx, nil
}

// NewRootCommand creates the root cobra command for the xyncra-client CLI.
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "xyncra-client",
		Short: "Xyncra messaging client CLI",
	}

	// Global persistent flags (D-034: flag > env var > default).
	cmd.PersistentFlags().StringP("user-id", "u", "", "User ID (env: XYNCRA_USER_ID)")
	cmd.PersistentFlags().String("device-id", defaultDeviceID(), "Device ID (env: XYNCRA_DEVICE_ID)")
	cmd.PersistentFlags().StringP("server", "s", "ws://localhost:8080/ws", "Server URL (env: XYNCRA_SERVER)")
	cmd.PersistentFlags().String("db-path", "", "Database path (default: $USER_DIR/xyncra.db) (env: XYNCRA_DB_PATH)")
	cmd.PersistentFlags().String("log-dir", "", "Log directory (default: $USER_DIR/logs/) (env: XYNCRA_LOG_DIR)")

	// Subcommands — defined in listen.go and send.go.
	cmd.AddCommand(newListenCommand())
	cmd.AddCommand(newSendCommand())

	return cmd
}
