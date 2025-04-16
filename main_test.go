package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

func TestMain(t *testing.T) {
	// Save original args and restore them after the test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set up test args with an invalid flag to trigger an error
	os.Args = []string{"dependabot-pr-checker", "--invalid-flag"}

	// Capture stdout
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		w.Close()
	}()

	// Override osExit
	var wg sync.WaitGroup
	wg.Add(1)
	exitCalled := false
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) {
		exitCalled = true
		wg.Done()
	}

	// Call main in a separate goroutine
	go main()

	// Wait for osExit to be called or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// osExit was called
	case <-time.After(2 * time.Second):
		// Timeout
		t.Error("Timeout waiting for os.Exit to be called")
	}

	// Check if exit was called
	if !exitCalled {
		t.Error("Expected os.Exit to be called")
	}
}
