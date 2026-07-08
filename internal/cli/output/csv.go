package output

import (
	"io"
	"os"
)

// nopCloseWriter wraps an io.Writer with a no-op Close method.
type nopCloseWriter struct {
	io.Writer
}

// Close is a no-op for nopCloseWriter.
func (n *nopCloseWriter) Close() error { return nil }

// OpenExportOutput opens an output destination for export data.
// If path is empty or "-", returns a wrapper around os.Stdout whose Close is a no-op.
// Otherwise creates or truncates the file at path.
func OpenExportOutput(path string) (io.WriteCloser, error) {
	if path == "" || path == "-" {
		return &nopCloseWriter{Writer: os.Stdout}, nil
	}
	return os.Create(path)
}
