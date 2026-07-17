package output

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Stdout tests (path == "" or "-")
// ---------------------------------------------------------------------------

func TestOpenExportOutput_Stdout_EmptyPath(t *testing.T) {
	wc, err := OpenExportOutput("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = wc.Close() }()

	// Should be a nopCloseWriter wrapping os.Stdout.
	if _, ok := wc.(*nopCloseWriter); !ok {
		t.Errorf("expected *nopCloseWriter, got %T", wc)
	}
}

func TestOpenExportOutput_Stdout_DashPath(t *testing.T) {
	wc, err := OpenExportOutput("-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = wc.Close() }()

	// Should be a nopCloseWriter wrapping os.Stdout.
	if _, ok := wc.(*nopCloseWriter); !ok {
		t.Errorf("expected *nopCloseWriter, got %T", wc)
	}
}

func TestOpenExportOutput_Stdout_CloseIsNoop(t *testing.T) {
	wc, err := OpenExportOutput("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close should not return an error (no-op).
	if err := wc.Close(); err != nil {
		t.Errorf("expected Close() to return nil, got %v", err)
	}

	// Calling Close multiple times should also be fine.
	if err := wc.Close(); err != nil {
		t.Errorf("expected second Close() to return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// File tests
// ---------------------------------------------------------------------------

func TestOpenExportOutput_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.csv")

	wc, err := OpenExportOutput(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be an *os.File (not nopCloseWriter).
	if _, ok := wc.(*os.File); !ok {
		t.Errorf("expected *os.File, got %T", wc)
	}

	// File should exist after OpenExportOutput.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %q to exist", path)
	}

	_ = wc.Close()
}

func TestOpenExportOutput_File_WriteAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")

	wc, err := OpenExportOutput(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := "id,name,status\n1,Alice,active\n"
	_, err = io.WriteString(wc, data)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	if err := wc.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	// Read back and verify content.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(content) != data {
		t.Errorf("expected file content %q, got %q", data, string(content))
	}
}

func TestOpenExportOutput_File_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overwrite.csv")

	// Pre-create the file with some content.
	if err := os.WriteFile(path, []byte("old content"), 0644); err != nil {
		t.Fatalf("failed to pre-create file: %v", err)
	}

	wc, err := OpenExportOutput(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newData := "new content"
	_, err = io.WriteString(wc, newData)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	if err := wc.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	// Read back — should only contain new content (old content is gone).
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(content) != newData {
		t.Errorf("expected file content %q, got %q", newData, string(content))
	}
}
