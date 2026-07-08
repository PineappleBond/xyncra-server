// Package output provides formatted console output utilities for the Xyncra CLI.
// It uses only standard library packages (text/tabwriter, encoding/json) per D-041.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// ConsoleWriter writes formatted output to an io.Writer.
type ConsoleWriter struct {
	w io.Writer
}

// KeyValueEntry represents a key-value pair for aligned display.
type KeyValueEntry struct {
	Key   string
	Value string
}

// NewConsoleWriter creates a ConsoleWriter that writes to w.
func NewConsoleWriter(w io.Writer) *ConsoleWriter {
	return &ConsoleWriter{w: w}
}

// Table writes a tabular output with headers and rows using text/tabwriter.
// Headers are printed first, followed by a separator line (---), then the data rows.
// If rows is empty, only headers and the separator line are printed.
func (cw *ConsoleWriter) Table(headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(cw.w, 0, 0, 2, ' ', 0)

	// Print headers separated by tabs.
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(tw, "\t")
		}
		fmt.Fprint(tw, h)
	}
	fmt.Fprintln(tw)

	// Print separator line.
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(tw, "\t")
		}
		for j := 0; j < len(h); j++ {
			fmt.Fprint(tw, "-")
		}
	}
	fmt.Fprintln(tw)

	// Print data rows.
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(tw, "\t")
			}
			fmt.Fprint(tw, cell)
		}
		fmt.Fprintln(tw)
	}

	tw.Flush()
}

// KeyValue writes aligned key-value pairs. Keys are left-aligned and padded
// to the length of the longest key, followed by ": " and the value.
func (cw *ConsoleWriter) KeyValue(pairs []KeyValueEntry) {
	if len(pairs) == 0 {
		return
	}

	// Find the longest key length for alignment.
	maxKeyLen := 0
	for _, p := range pairs {
		if len(p.Key) > maxKeyLen {
			maxKeyLen = len(p.Key)
		}
	}

	for _, p := range pairs {
		fmt.Fprintf(cw.w, "%-*s  %s\n", maxKeyLen, p.Key, p.Value)
	}
}

// JSONPretty writes v as indented JSON to the underlying writer, followed by a newline.
// It returns an error if JSON marshalling fails.
func (cw *ConsoleWriter) JSONPretty(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = cw.w.Write(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cw.w)
	return err
}

// Section writes a section title followed by a newline.
func (cw *ConsoleWriter) Section(title string) {
	fmt.Fprintln(cw.w, title)
}

// Info writes an informational message followed by a newline.
func (cw *ConsoleWriter) Info(msg string) {
	fmt.Fprintln(cw.w, msg)
}

// Error writes an error message in the format "Error: msg\n".
func (cw *ConsoleWriter) Error(msg string) {
	fmt.Fprintf(cw.w, "Error: %s\n", msg)
}
