package cli

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// captureStdout captures stdout output during the execution of fn.
// It returns whatever was written to stdout.
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w

	var mu sync.Mutex
	var buf bytes.Buffer
	done := make(chan struct{})

	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stdout = old
	<-done

	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

// captureStderr captures stderr output during the execution of fn.
// It returns whatever was written to stderr.
func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stderr = w

	var mu sync.Mutex
	var buf bytes.Buffer
	done := make(chan struct{})

	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stderr = old
	<-done

	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}
