package cli

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseTimeArg tests
// ---------------------------------------------------------------------------

func TestParseTimeArg_Duration_1h(t *testing.T) {
	before := time.Now()
	got, err := parseTimeArg("1h")
	if err != nil {
		t.Fatalf("parseTimeArg(\"1h\") error: %v", err)
	}
	want := before.Add(-1 * time.Hour)
	// Allow 2 seconds tolerance for execution time.
	if diff := got.Sub(want); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("parseTimeArg(\"1h\") = %v, want ~%v (diff=%v)", got, want, diff)
	}
}

func TestParseTimeArg_Duration_30m(t *testing.T) {
	before := time.Now()
	got, err := parseTimeArg("30m")
	if err != nil {
		t.Fatalf("parseTimeArg(\"30m\") error: %v", err)
	}
	want := before.Add(-30 * time.Minute)
	if diff := got.Sub(want); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("parseTimeArg(\"30m\") = %v, want ~%v (diff=%v)", got, want, diff)
	}
}

func TestParseTimeArg_Days_7d(t *testing.T) {
	before := time.Now()
	got, err := parseTimeArg("7d")
	if err != nil {
		t.Fatalf("parseTimeArg(\"7d\") error: %v", err)
	}
	want := before.Add(-7 * 24 * time.Hour)
	if diff := got.Sub(want); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("parseTimeArg(\"7d\") = %v, want ~%v (diff=%v)", got, want, diff)
	}
}

func TestParseTimeArg_RFC3339(t *testing.T) {
	got, err := parseTimeArg("2026-07-09T10:00:00Z")
	if err != nil {
		t.Fatalf("parseTimeArg(\"2026-07-09T10:00:00Z\") error: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-07-09T10:00:00Z")
	if !got.Equal(want) {
		t.Errorf("parseTimeArg(\"2026-07-09T10:00:00Z\") = %v, want %v", got, want)
	}
}

func TestParseTimeArg_Invalid(t *testing.T) {
	_, err := parseTimeArg("invalid")
	if err == nil {
		t.Fatal("parseTimeArg(\"invalid\") should return error")
	}
}

func TestParseTimeArg_Empty(t *testing.T) {
	_, err := parseTimeArg("")
	if err == nil {
		t.Fatal("parseTimeArg(\"\") should return error")
	}
}

// ---------------------------------------------------------------------------
// logs command structure tests
// ---------------------------------------------------------------------------

func TestNewLogsCommand_HasSubcommands(t *testing.T) {
	cmd := newLogsCommand()
	expected := []string{"tail", "search", "stats", "export", "cleanup"}
	found := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		found[sub.Name()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestNewLogsTailCommand_HasFlags(t *testing.T) {
	cmd := newLogsTailCommand()
	flags := []string{"type", "limit", "since"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestNewLogsSearchCommand_HasFlags(t *testing.T) {
	cmd := newLogsSearchCommand()
	flags := []string{"type", "method", "error", "from", "to", "conversation-id", "request-id", "limit"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestNewLogsStatsCommand_HasFlags(t *testing.T) {
	cmd := newLogsStatsCommand()
	flags := []string{"since", "interval"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestNewLogsExportCommand_HasFlags(t *testing.T) {
	cmd := newLogsExportCommand()
	flags := []string{"type", "format", "output", "method", "from", "to"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestNewLogsCleanupCommand_HasFlags(t *testing.T) {
	cmd := newLogsCleanupCommand()
	flags := []string{"retain", "dry-run", "type"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

// ---------------------------------------------------------------------------
// parseDurationArg tests
// ---------------------------------------------------------------------------

func TestParseDurationArg_StandardDuration(t *testing.T) {
	got, err := parseDurationArg("5s")
	if err != nil {
		t.Fatalf("parseDurationArg(\"5s\") error: %v", err)
	}
	if got != 5*time.Second {
		t.Errorf("parseDurationArg(\"5s\") = %v, want %v", got, 5*time.Second)
	}
}

func TestParseDurationArg_Days(t *testing.T) {
	got, err := parseDurationArg("7d")
	if err != nil {
		t.Fatalf("parseDurationArg(\"7d\") error: %v", err)
	}
	if got != 7*24*time.Hour {
		t.Errorf("parseDurationArg(\"7d\") = %v, want %v", got, 7*24*time.Hour)
	}
}

func TestParseDurationArg_Hours(t *testing.T) {
	got, err := parseDurationArg("168h")
	if err != nil {
		t.Fatalf("parseDurationArg(\"168h\") error: %v", err)
	}
	if got != 168*time.Hour {
		t.Errorf("parseDurationArg(\"168h\") = %v, want %v", got, 168*time.Hour)
	}
}

func TestParseDurationArg_Invalid(t *testing.T) {
	_, err := parseDurationArg("invalid")
	if err == nil {
		t.Fatal("parseDurationArg(\"invalid\") should return error")
	}
}

func TestParseDurationArg_ZeroDays(t *testing.T) {
	_, err := parseDurationArg("0d")
	if err == nil {
		t.Fatal("parseDurationArg(\"0d\") should return error")
	}
}

// ---------------------------------------------------------------------------
// parseTimeArg edge case tests
// ---------------------------------------------------------------------------

func TestParseTimeArg_ZeroDays(t *testing.T) {
	_, err := parseTimeArg("0d")
	if err == nil {
		t.Fatal("parseTimeArg(\"0d\") should return error")
	}
}

func TestParseTimeArg_Seconds(t *testing.T) {
	before := time.Now()
	got, err := parseTimeArg("5s")
	if err != nil {
		t.Fatalf("parseTimeArg(\"5s\") error: %v", err)
	}
	want := before.Add(-5 * time.Second)
	if diff := got.Sub(want); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("parseTimeArg(\"5s\") = %v, want ~%v (diff=%v)", got, want, diff)
	}
}

func TestParseTimeArg_StandardDuration10m(t *testing.T) {
	before := time.Now()
	got, err := parseTimeArg("10m")
	if err != nil {
		t.Fatalf("parseTimeArg(\"10m\") error: %v", err)
	}
	want := before.Add(-10 * time.Minute)
	if diff := got.Sub(want); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("parseTimeArg(\"10m\") = %v, want ~%v (diff=%v)", got, want, diff)
	}
}

// ---------------------------------------------------------------------------
// logs export command --limit flag test
// ---------------------------------------------------------------------------

func TestNewLogsExportCommand_HasLimitFlag(t *testing.T) {
	cmd := newLogsExportCommand()
	flag := cmd.Flags().Lookup("limit")
	if flag == nil {
		t.Fatal("missing --limit flag")
	}
	if flag.DefValue != "1000" {
		t.Errorf("--limit default = %q, want %q", flag.DefValue, "1000")
	}
}
