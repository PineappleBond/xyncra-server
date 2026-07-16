package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// reload-agents (IPC-only, D-076)
// ---------------------------------------------------------------------------

// newReloadAgentsCommand creates the "reload-agents" subcommand.
// D-076: Reload agent configurations from disk.
// IPC-only (D-036) — the daemon manages agent config state; if the daemon
// is not running there is nothing to reload.
func newReloadAgentsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reload-agents",
		Short: "Reload agent configurations from disk (IPC-only, D-076)",
		Long: `Reload agent configurations from disk. The daemon picks up
any changes to agent definition files without requiring a restart.

This command is IPC-only (D-036, D-076) — it requires the listen daemon
to be running. Start the daemon with 'xyncra-client listen' first.`,
		RunE: runReloadAgents,
	}
}

// runReloadAgents is the entry point for "reload-agents".
func runReloadAgents(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("reload-agents: %w", err)
	}

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	resp, err := ipcClient.Call(ctx, "reload_agents", nil)
	if err != nil {
		// IPC connection failed — daemon is not running.
		// D-036/D-042: exit code 2 = precondition not met.
		fmt.Fprintln(os.Stderr, "Error: daemon not running.")
		fmt.Fprintf(os.Stderr, "Hint: Start with 'xyncra-client listen --user-id %s'\n", cliCtx.UserID)
		os.Exit(2)
	}

	if resp.Error != nil {
		return fmt.Errorf("reload-agents: %s", resp.Error.Message)
	}

	var result map[string]int
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("reload-agents: unmarshal result: %w", err)
	}

	fmt.Printf("Successfully reloaded %d agent configuration(s)\n", result["count"])
	return nil
}
