package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// sync-updates (IPC-only, D-036)
// ---------------------------------------------------------------------------

// newSyncUpdatesCommand creates the "sync-updates" subcommand.
// D-036: this command is IPC-only — there is no standalone WebSocket fallback
// because opening a second WebSocket connection would compete with the daemon's
// syncManager for SQLite writes and localMaxSeq state.
func newSyncUpdatesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-updates",
		Short: "Trigger a full sync of updates from the server via the daemon",
		RunE:  runSyncUpdates,
	}
}

// runSyncUpdates is the entry point for "sync-updates". It sends a JSON-RPC
// request to the running daemon to trigger a FullSync.
func runSyncUpdates(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("sync-updates: %w", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	resp, err := ipcClient.Call(ctx, "sync_updates", nil)
	if err != nil {
		// IPC connection failed — daemon is not running.
		fmt.Fprintln(os.Stderr, "Error: daemon not running.")
		fmt.Fprintln(os.Stderr, "Hint: Start with 'xyncra-client listen --user-id <user>'")
		return fmt.Errorf("sync-updates: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("sync-updates: %s", resp.Error.Message)
	}

	fmt.Println("Sync complete.")
	return nil
}
