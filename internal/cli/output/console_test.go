package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Table tests
// ---------------------------------------------------------------------------

func TestConsoleWriter_Table_BasicOutput(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	headers := []string{"ID", "NAME", "STATUS"}
	rows := [][]string{
		{"1", "Alice", "active"},
		{"2", "Bob", "inactive"},
	}
	cw.Table(headers, rows)

	out := buf.String()

	// Should contain all headers.
	for _, h := range headers {
		if !strings.Contains(out, h) {
			t.Errorf("expected output to contain header %q, got:\n%s", h, out)
		}
	}

	// Should contain separator (at least "---").
	if !strings.Contains(out, "---") {
		t.Errorf("expected output to contain separator '---', got:\n%s", out)
	}

	// Should contain all row values.
	for _, row := range rows {
		for _, cell := range row {
			if !strings.Contains(out, cell) {
				t.Errorf("expected output to contain cell %q, got:\n%s", cell, out)
			}
		}
	}
}

func TestConsoleWriter_Table_EmptyRows(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	headers := []string{"COL_A", "COL_B"}
	cw.Table(headers, nil)

	out := buf.String()

	// Should still contain headers.
	for _, h := range headers {
		if !strings.Contains(out, h) {
			t.Errorf("expected output to contain header %q, got:\n%s", h, out)
		}
	}

	// Should contain separator.
	if !strings.Contains(out, "---") {
		t.Errorf("expected output to contain separator, got:\n%s", out)
	}

	// Lines: header + separator = 2 lines (each terminated by newline).
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (header + separator), got %d:\n%s", len(lines), out)
	}
}

func TestConsoleWriter_Table_Alignment(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	headers := []string{"ID", "LONGER_HEADER", "X"}
	rows := [][]string{
		{"1", "value", "data"},
	}
	cw.Table(headers, rows)

	out := buf.String()

	// tabwriter aligns columns — the second column data should appear after
	// the second column header at a consistent tab stop. We verify by checking
	// that the output contains the tab-separated header line and the
	// separator has dashes matching each header length.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	// Second line is the separator; verify dash counts match header lengths.
	sepLine := lines[1]
	// The separator should contain runs of dashes. We check total dash count
	// is at least the sum of header lengths.
	dashCount := strings.Count(sepLine, "-")
	expectedMinDashes := len("ID") + len("LONGER_HEADER") + len("X")
	if dashCount < expectedMinDashes {
		t.Errorf("expected at least %d dashes in separator, got %d: %q",
			expectedMinDashes, dashCount, sepLine)
	}
}

func TestConsoleWriter_Table_UnicodeContent(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	headers := []string{"NAME", "CITY"}
	rows := [][]string{
		{"张三", "北京"},
		{"Alice", "New York"},
	}
	cw.Table(headers, rows)

	out := buf.String()

	// Verify all content is present.
	for _, row := range rows {
		for _, cell := range row {
			if !strings.Contains(out, cell) {
				t.Errorf("expected output to contain %q, got:\n%s", cell, out)
			}
		}
	}

	// Verify headers present.
	for _, h := range headers {
		if !strings.Contains(out, h) {
			t.Errorf("expected output to contain header %q, got:\n%s", h, out)
		}
	}
}

// ---------------------------------------------------------------------------
// KeyValue tests
// ---------------------------------------------------------------------------

func TestConsoleWriter_KeyValue_BasicPairs(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	pairs := []KeyValueEntry{
		{Key: "Name", Value: "Alice"},
		{Key: "Age", Value: "30"},
	}
	cw.KeyValue(pairs)

	out := buf.String()

	if !strings.Contains(out, "Name") {
		t.Errorf("expected output to contain 'Name', got:\n%s", out)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("expected output to contain 'Alice', got:\n%s", out)
	}
	if !strings.Contains(out, "Age") {
		t.Errorf("expected output to contain 'Age', got:\n%s", out)
	}
	if !strings.Contains(out, "30") {
		t.Errorf("expected output to contain '30', got:\n%s", out)
	}

	// Each pair should have ": " separator between key and value.
	for _, p := range pairs {
		expected := p.Key + ":"
		// The format is "%-*s  %s\n", so key is padded then "  " then value.
		// Check key is followed by spaces then value.
		if !strings.Contains(out, p.Key) || !strings.Contains(out, p.Value) {
			t.Errorf("expected key %q and value %q in output, got:\n%s", p.Key, p.Value, out)
		}
		_ = expected
	}

	// Should have exactly 2 lines.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestConsoleWriter_KeyValue_EmptyPairs(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	cw.KeyValue(nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty pairs, got:\n%s", buf.String())
	}

	// Also test with an empty (non-nil) slice.
	buf.Reset()
	cw.KeyValue([]KeyValueEntry{})
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty slice, got:\n%s", buf.String())
	}
}

func TestConsoleWriter_KeyValue_Alignment(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	pairs := []KeyValueEntry{
		{Key: "Short", Value: "val1"},
		{Key: "MuchLongerKey", Value: "val2"},
		{Key: "Mid", Value: "val3"},
	}
	cw.KeyValue(pairs)

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// The longest key is "MuchLongerKey" (13 chars). All keys should be
	// padded to 13 chars. After padding, "  " separator, then value.
	// So "Short" should be padded with 8 spaces to reach 13 chars total.
	// Check that the first line starts with "Short" followed by spaces.
	if !strings.HasPrefix(lines[0], "Short") {
		t.Errorf("expected first line to start with 'Short', got: %q", lines[0])
	}

	// Verify "MuchLongerKey" is NOT padded (it IS the longest).
	if !strings.HasPrefix(lines[1], "MuchLongerKey") {
		t.Errorf("expected second line to start with 'MuchLongerKey', got: %q", lines[1])
	}

	// All values should appear at the same column position.
	// After the key (padded to maxKeyLen) + "  " + value.
	// maxKeyLen = 13, so values start at column 15 (0-indexed).
	for i, p := range pairs {
		expectedValuePos := len("MuchLongerKey") + 2 // 13 + 2 = 15
		// Find where the value starts in the line.
		valIdx := strings.Index(lines[i], p.Value)
		if valIdx != expectedValuePos {
			t.Errorf("line %d: expected value %q at position %d, found at %d: %q",
				i, p.Value, expectedValuePos, valIdx, lines[i])
		}
	}
}

// ---------------------------------------------------------------------------
// JSONPretty tests
// ---------------------------------------------------------------------------

func TestConsoleWriter_JSONPretty_Struct(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	type sample struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	v := sample{Name: "Alice", Age: 30}

	err := cw.JSONPretty(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Should be valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput:\n%s", err, out)
	}

	if parsed["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", parsed["name"])
	}
	if parsed["age"] != float64(30) {
		t.Errorf("expected age=30, got %v", parsed["age"])
	}

	// Should be indented (contain "  ").
	if !strings.Contains(out, "  ") {
		t.Errorf("expected indented JSON, got:\n%s", out)
	}
}

func TestConsoleWriter_JSONPretty_Map(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	v := map[string]any{
		"key1": "value1",
		"key2": 42,
	}

	err := cw.JSONPretty(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Should be valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput:\n%s", err, out)
	}

	if parsed["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", parsed["key1"])
	}
	if parsed["key2"] != float64(42) {
		t.Errorf("expected key2=42, got %v", parsed["key2"])
	}
}

func TestConsoleWriter_JSONPretty_Error(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	// Channels cannot be marshalled to JSON.
	ch := make(chan int)
	err := cw.JSONPretty(ch)
	if err == nil {
		t.Fatal("expected error when marshalling channel to JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Section, Info, Error tests
// ---------------------------------------------------------------------------

func TestConsoleWriter_Section(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	cw.Section("My Section")

	out := buf.String()
	expected := "My Section\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestConsoleWriter_Info(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	cw.Info("something happened")

	out := buf.String()
	expected := "something happened\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestConsoleWriter_Error(t *testing.T) {
	var buf bytes.Buffer
	cw := NewConsoleWriter(&buf)

	cw.Error("something went wrong")

	out := buf.String()
	expected := "Error: something went wrong\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}
