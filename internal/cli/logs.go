package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/internal/cli/output"
	"github.com/PineappleBond/xyncra-server/pkg/store"
)

// newLogsCommand creates the "logs" parent command with five subcommands:
// tail, search, stats, export, cleanup.
func newLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View and manage RPC and notification logs",
	}

	cmd.AddCommand(newLogsTailCommand())
	cmd.AddCommand(newLogsSearchCommand())
	cmd.AddCommand(newLogsStatsCommand())
	cmd.AddCommand(newLogsExportCommand())
	cmd.AddCommand(newLogsCleanupCommand())

	return cmd
}

// parseTimeArg parses a time argument supporting relative durations
// (e.g. "5m", "1h", "168h", "7d") and absolute RFC3339 timestamps.
func parseTimeArg(s string) (time.Time, error) {
	// Try standard Go duration first.
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}

	// Try "{n}d" format (days).
	if numStr, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			return time.Now().Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}

	// Try absolute RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time argument %q: use duration (e.g. 1h, 7d) or RFC3339", s)
}

// parseDurationArg parses a duration argument supporting standard Go durations
// (e.g. "5m", "1h", "168h") and "{n}d" day format (e.g. "7d").
func parseDurationArg(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if numStr, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	return 0, fmt.Errorf("invalid duration %q: use Go duration (e.g. 1h, 168h) or days (e.g. 7d)", s)
}

// ---------------------------------------------------------------------------
// logs tail
// ---------------------------------------------------------------------------

// newLogsTailCommand creates the "logs tail" subcommand.
func newLogsTailCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Show recent log entries",
		RunE:  runLogsTail,
	}

	cmd.Flags().String("type", "rpc", "Log type: rpc or notifications")
	cmd.Flags().Int("limit", 50, "Maximum number of entries to show")
	cmd.Flags().String("since", "1h", "Show entries since (e.g. 1h, 30m, 7d)")

	return cmd
}

// runLogsTail shows recent log entries from the local database.
func runLogsTail(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("logs tail: %w", err)
	}

	logType, _ := cmd.Flags().GetString("type")
	limit, _ := cmd.Flags().GetInt("limit")
	sinceStr, _ := cmd.Flags().GetString("since")

	since, err := parseTimeArg(sinceStr)
	if err != nil {
		return fmt.Errorf("logs tail: %w", err)
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("logs tail: open db: %w", err)
	}
	defer db.Close()

	cw := output.NewConsoleWriter(os.Stdout)

	switch logType {
	case "rpc":
		filter := store.RPCLogFilter{
			StartTime: &since,
			Limit:     limit,
		}
		logs, err := db.RPCLogs.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("logs tail: %w", err)
		}

		headers := []string{"TIME", "METHOD", "STATUS", "DURATION", "CONVERSATION"}
		var rows [][]string
		for _, l := range logs {
			rows = append(rows, []string{
				l.CreatedAt.Format(time.RFC3339),
				l.Method,
				fmt.Sprintf("%d", l.StatusCode),
				fmt.Sprintf("%.3fms", float64(l.Duration.Microseconds())/1000.0),
				l.ConversationID,
			})
		}
		cw.Table(headers, rows)

	case "notifications":
		filter := store.NotificationLogFilter{
			StartTime: &since,
			Limit:     limit,
		}
		logs, err := db.NotificationLogs.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("logs tail: %w", err)
		}

		headers := []string{"TIME", "SEQ", "TYPE"}
		var rows [][]string
		for _, l := range logs {
			rows = append(rows, []string{
				l.CreatedAt.Format(time.RFC3339),
				fmt.Sprintf("%d", l.Seq),
				l.Type,
			})
		}
		cw.Table(headers, rows)

	default:
		return fmt.Errorf("logs tail: invalid type %q: must be rpc or notifications", logType)
	}

	return nil
}

// ---------------------------------------------------------------------------
// logs search
// ---------------------------------------------------------------------------

// newLogsSearchCommand creates the "logs search" subcommand.
func newLogsSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search log entries with filters",
		RunE:  runLogsSearch,
	}

	cmd.Flags().String("type", "rpc", "Log type: rpc or notifications")
	cmd.Flags().String("method", "", "Filter by RPC method")
	cmd.Flags().Bool("error", false, "Show only error entries")
	cmd.Flags().String("from", "", "Start time (duration or RFC3339)")
	cmd.Flags().String("to", "", "End time (duration or RFC3339)")
	cmd.Flags().String("conversation-id", "", "Filter by conversation ID (RPC only)")
	cmd.Flags().String("request-id", "", "Get specific entry by request ID (RPC only)")
	cmd.Flags().Int("limit", 100, "Maximum number of entries to return")

	return cmd
}

// runLogsSearch searches log entries with the given filters.
func runLogsSearch(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("logs search: %w", err)
	}

	logType, _ := cmd.Flags().GetString("type")
	method, _ := cmd.Flags().GetString("method")
	errOnly, _ := cmd.Flags().GetBool("error")
	fromStr, _ := cmd.Flags().GetString("from")
	toStr, _ := cmd.Flags().GetString("to")
	convID, _ := cmd.Flags().GetString("conversation-id")
	reqID, _ := cmd.Flags().GetString("request-id")
	limit, _ := cmd.Flags().GetInt("limit")

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("logs search: open db: %w", err)
	}
	defer db.Close()

	cw := output.NewConsoleWriter(os.Stdout)

	switch logType {
	case "rpc":
		// Special path: lookup by request ID.
		if reqID != "" {
			log, err := db.RPCLogs.GetByRequestID(ctx, reqID)
			if err != nil {
				return fmt.Errorf("logs search: %w", err)
			}

			headers := []string{"TIME", "METHOD", "STATUS", "DURATION", "CONVERSATION"}
			rows := [][]string{{
				log.CreatedAt.Format(time.RFC3339),
				log.Method,
				fmt.Sprintf("%d", log.StatusCode),
				fmt.Sprintf("%.3fms", float64(log.Duration.Microseconds())/1000.0),
				log.ConversationID,
			}}
			cw.Table(headers, rows)
			return nil
		}

		filter := store.RPCLogFilter{
			Method:         method,
			ConversationID: convID,
			Limit:          limit,
		}

		if fromStr != "" {
			t, err := parseTimeArg(fromStr)
			if err != nil {
				return fmt.Errorf("logs search: %w", err)
			}
			filter.StartTime = &t
		}
		if toStr != "" {
			t, err := parseTimeArg(toStr)
			if err != nil {
				return fmt.Errorf("logs search: %w", err)
			}
			filter.EndTime = &t
		}
		if errOnly {
			sc := -1
			filter.StatusCode = &sc
		}

		logs, err := db.RPCLogs.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("logs search: %w", err)
		}

		headers := []string{"TIME", "METHOD", "STATUS", "DURATION", "CONVERSATION"}
		var rows [][]string
		for _, l := range logs {
			rows = append(rows, []string{
				l.CreatedAt.Format(time.RFC3339),
				l.Method,
				fmt.Sprintf("%d", l.StatusCode),
				fmt.Sprintf("%.3fms", float64(l.Duration.Microseconds())/1000.0),
				l.ConversationID,
			})
		}
		cw.Table(headers, rows)

	case "notifications":
		filter := store.NotificationLogFilter{
			Limit: limit,
		}

		if fromStr != "" {
			t, err := parseTimeArg(fromStr)
			if err != nil {
				return fmt.Errorf("logs search: %w", err)
			}
			filter.StartTime = &t
		}
		if toStr != "" {
			t, err := parseTimeArg(toStr)
			if err != nil {
				return fmt.Errorf("logs search: %w", err)
			}
			filter.EndTime = &t
		}

		logs, err := db.NotificationLogs.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("logs search: %w", err)
		}

		headers := []string{"TIME", "SEQ", "TYPE"}
		var rows [][]string
		for _, l := range logs {
			rows = append(rows, []string{
				l.CreatedAt.Format(time.RFC3339),
				fmt.Sprintf("%d", l.Seq),
				l.Type,
			})
		}
		cw.Table(headers, rows)

	default:
		return fmt.Errorf("logs search: invalid type %q: must be rpc or notifications", logType)
	}

	return nil
}

// ---------------------------------------------------------------------------
// logs stats
// ---------------------------------------------------------------------------

// newLogsStatsCommand creates the "logs stats" subcommand.
func newLogsStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show RPC log statistics",
		RunE:  runLogsStats,
	}

	cmd.Flags().String("since", "24h", "Statistics time window (e.g. 1h, 24h, 7d)")
	cmd.Flags().String("interval", "", "Group by interval: 1m, 5m, 15m, 1h, 1d")

	return cmd
}

// runLogsStats shows RPC log statistics.
func runLogsStats(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("logs stats: %w", err)
	}

	sinceStr, _ := cmd.Flags().GetString("since")
	interval, _ := cmd.Flags().GetString("interval")

	since, err := parseTimeArg(sinceStr)
	if err != nil {
		return fmt.Errorf("logs stats: %w", err)
	}
	now := time.Now()

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("logs stats: open db: %w", err)
	}
	defer db.Close()

	cw := output.NewConsoleWriter(os.Stdout)

	if interval == "" {
		// Aggregate without interval.
		rows, err := db.RPCLogs.Aggregate(ctx, since, now)
		if err != nil {
			return fmt.Errorf("logs stats: %w", err)
		}

		headers := []string{"METHOD", "COUNT", "SUCCESS", "ERRORS", "AVG (ms)"}
		var tableRows [][]string
		for _, r := range rows {
			tableRows = append(tableRows, []string{
				r.Method,
				fmt.Sprintf("%d", r.Count),
				fmt.Sprintf("%d", r.Success),
				fmt.Sprintf("%d", r.ErrorCount),
				fmt.Sprintf("%.3f", r.AvgMs),
			})
		}
		cw.Table(headers, tableRows)
	} else {
		// Validate interval.
		switch interval {
		case "1m", "5m", "15m", "1h", "1d":
		default:
			return fmt.Errorf("logs stats: invalid interval %q: must be one of 1m, 5m, 15m, 1h, 1d", interval)
		}

		rows, err := db.RPCLogs.AggregateByInterval(ctx, since, now, interval)
		if err != nil {
			return fmt.Errorf("logs stats: %w", err)
		}

		headers := []string{"INTERVAL", "METHOD", "COUNT", "SUCCESS", "ERRORS", "AVG (ms)"}
		var tableRows [][]string
		for _, r := range rows {
			tableRows = append(tableRows, []string{
				r.Interval,
				r.Method,
				fmt.Sprintf("%d", r.Count),
				fmt.Sprintf("%d", r.Success),
				fmt.Sprintf("%d", r.ErrorCount),
				fmt.Sprintf("%.3f", r.AvgMs),
			})
		}
		cw.Table(headers, tableRows)
	}

	return nil
}

// ---------------------------------------------------------------------------
// logs export
// ---------------------------------------------------------------------------

// newLogsExportCommand creates the "logs export" subcommand.
func newLogsExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export logs to CSV or JSON",
		RunE:  runLogsExport,
	}

	cmd.Flags().String("type", "rpc", "Log type: rpc or notifications")
	cmd.Flags().String("format", "csv", "Export format: csv or json")
	cmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")
	cmd.Flags().String("method", "", "Filter by RPC method (RPC only)")
	cmd.Flags().String("from", "", "Start time (duration or RFC3339)")
	cmd.Flags().String("to", "", "End time (duration or RFC3339)")
	cmd.Flags().Int("limit", 1000, "Maximum number of entries to export (max 10000)")

	return cmd
}

// runLogsExport exports log entries to CSV or JSON format.
func runLogsExport(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("logs export: %w", err)
	}

	logType, _ := cmd.Flags().GetString("type")
	format, _ := cmd.Flags().GetString("format")
	outputPath, _ := cmd.Flags().GetString("output")
	method, _ := cmd.Flags().GetString("method")
	fromStr, _ := cmd.Flags().GetString("from")
	toStr, _ := cmd.Flags().GetString("to")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("logs export: open db: %w", err)
	}
	defer db.Close()

	out, err := output.OpenExportOutput(outputPath)
	if err != nil {
		return fmt.Errorf("logs export: open output: %w", err)
	}
	defer out.Close()

	switch logType {
	case "rpc":
		filter := store.RPCLogFilter{
			Method: method,
			Limit:  limit,
		}
		if fromStr != "" {
			t, err := parseTimeArg(fromStr)
			if err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
			filter.StartTime = &t
		}
		if toStr != "" {
			t, err := parseTimeArg(toStr)
			if err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
			filter.EndTime = &t
		}

		switch format {
		case "csv":
			if err := db.RPCLogs.ExportCSV(ctx, out, filter); err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
		case "json":
			if err := db.RPCLogs.ExportJSON(ctx, out, filter); err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
		default:
			return fmt.Errorf("logs export: invalid format %q: must be csv or json", format)
		}

	case "notifications":
		filter := store.NotificationLogFilter{
			Limit: limit,
		}
		if fromStr != "" {
			t, err := parseTimeArg(fromStr)
			if err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
			filter.StartTime = &t
		}
		if toStr != "" {
			t, err := parseTimeArg(toStr)
			if err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
			filter.EndTime = &t
		}

		switch format {
		case "csv":
			if err := db.NotificationLogs.ExportCSV(ctx, out, filter); err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
		case "json":
			if err := db.NotificationLogs.ExportJSON(ctx, out, filter); err != nil {
				return fmt.Errorf("logs export: %w", err)
			}
		default:
			return fmt.Errorf("logs export: invalid format %q: must be csv or json", format)
		}

	default:
		return fmt.Errorf("logs export: invalid type %q: must be rpc or notifications", logType)
	}

	if outputPath != "" && outputPath != "-" {
		fmt.Fprintf(os.Stderr, "Exported to %s\n", outputPath)
	}

	return nil
}

// ---------------------------------------------------------------------------
// logs cleanup
// ---------------------------------------------------------------------------

// newLogsCleanupCommand creates the "logs cleanup" subcommand.
func newLogsCleanupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete old log entries",
		RunE:  runLogsCleanup,
	}

	cmd.Flags().String("retain", "168h", "Retention duration (e.g. 168h, 7d)")
	cmd.Flags().Bool("dry-run", false, "Show what would be deleted without deleting")
	cmd.Flags().String("type", "all", "Log type to clean: rpc, notifications, or all")

	return cmd
}

// runLogsCleanup deletes old log entries based on retention policy.
func runLogsCleanup(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("logs cleanup: %w", err)
	}

	retainStr, _ := cmd.Flags().GetString("retain")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	logType, _ := cmd.Flags().GetString("type")

	retention, err := parseDurationArg(retainStr)
	if err != nil {
		return fmt.Errorf("logs cleanup: %w", err)
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("logs cleanup: open db: %w", err)
	}
	defer db.Close()

	cw := output.NewConsoleWriter(os.Stdout)
	before := time.Now().Add(-retention)

	switch logType {
	case "rpc":
		if dryRun {
			count, err := db.RPCLogs.CountBefore(ctx, before)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			cw.Info(fmt.Sprintf("Would delete %d RPC log entries older than %s", count, before.Format(time.RFC3339)))
		} else {
			count, err := db.RPCLogs.CleanupOlderThan(ctx, retention)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			cw.Info(fmt.Sprintf("Deleted %d RPC log entries.", count))
		}

	case "notifications":
		if dryRun {
			count, err := db.NotificationLogs.CountBefore(ctx, before)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			cw.Info(fmt.Sprintf("Would delete %d notification log entries older than %s", count, before.Format(time.RFC3339)))
		} else {
			count, err := db.NotificationLogs.CleanupBefore(ctx, before)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			cw.Info(fmt.Sprintf("Deleted %d notification log entries.", count))
		}

	case "all":
		if dryRun {
			rpcCount, err := db.RPCLogs.CountBefore(ctx, before)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			notifCount, err := db.NotificationLogs.CountBefore(ctx, before)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			total := rpcCount + notifCount
			cw.Info(fmt.Sprintf("Would delete %d log entries older than %s", total, before.Format(time.RFC3339)))
			cw.Info(fmt.Sprintf("  RPC logs: %d", rpcCount))
			cw.Info(fmt.Sprintf("  Notification logs: %d", notifCount))
		} else {
			rpcCount, err := db.RPCLogs.CleanupOlderThan(ctx, retention)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			notifCount, err := db.NotificationLogs.CleanupBefore(ctx, before)
			if err != nil {
				return fmt.Errorf("logs cleanup: %w", err)
			}
			total := rpcCount + notifCount
			cw.Info(fmt.Sprintf("Deleted %d log entries.", total))
			cw.Info(fmt.Sprintf("  RPC logs: %d", rpcCount))
			cw.Info(fmt.Sprintf("  Notification logs: %d", notifCount))
		}

	default:
		return fmt.Errorf("logs cleanup: invalid type %q: must be rpc, notifications, or all", logType)
	}

	return nil
}
