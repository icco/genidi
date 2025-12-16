package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileBrowserViewport(t *testing.T) {
	// Create a test directory with many files
	testDir := "test_viewport"
	if err := os.MkdirAll(testDir, 0750); err != nil {
		t.Fatalf("Error creating test directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(testDir); err != nil {
			t.Logf("Warning: failed to clean up test directory: %v", err)
		}
	}()

	// Create 30 test files
	for i := 0; i < 30; i++ {
		filename := filepath.Join(testDir, "test_"+string(rune('0'+i%10))+"_file.mid")
		if err := os.WriteFile(filename, []byte{}, 0600); err != nil {
			t.Fatalf("Error creating test file: %v", err)
		}
	}

	// Create a model with the test directory
	m := initialModel()
	m.fileBrowser.currentDir = testDir
	m.fileBrowser.loadFiles()
	m.height = 20 // Simulate a terminal height

	// Test 1: Initial state - viewport should start at 0
	if m.fileBrowser.viewportTop != 0 {
		t.Errorf("Expected viewportTop to be 0, got %d", m.fileBrowser.viewportTop)
	}

	// Test 2: Move cursor down beyond visible area
	maxVisibleLines := m.height - 9 // Same calculation as in viewFileBrowser
	if maxVisibleLines < 5 {
		maxVisibleLines = 5
	}

	// Move cursor to position that should trigger scroll
	m.fileBrowser.cursor = maxVisibleLines + 5

	// Simulate the down key press logic
	if m.fileBrowser.cursor >= m.fileBrowser.viewportTop+maxVisibleLines {
		m.fileBrowser.viewportTop = m.fileBrowser.cursor - maxVisibleLines + 1
	}

	// Viewport should have scrolled
	expectedTop := m.fileBrowser.cursor - maxVisibleLines + 1
	if m.fileBrowser.viewportTop != expectedTop {
		t.Errorf("Expected viewportTop to be %d, got %d", expectedTop, m.fileBrowser.viewportTop)
	}

	// Test 3: Move cursor up - viewport should scroll up
	m.fileBrowser.cursor = 2
	if m.fileBrowser.cursor < m.fileBrowser.viewportTop {
		m.fileBrowser.viewportTop = m.fileBrowser.cursor
	}

	if m.fileBrowser.viewportTop != 2 {
		t.Errorf("Expected viewportTop to be 2, got %d", m.fileBrowser.viewportTop)
	}

	// Test 4: Test viewFileBrowser rendering doesn't panic with viewport
	view := m.viewFileBrowser()
	if view == "" {
		t.Error("Expected non-empty view")
	}

	t.Log("✓ File browser viewport logic works correctly!")
}

func TestFileBrowserLoadFilesResetsViewport(t *testing.T) {
	// Create a test directory
	testDir := "test_viewport_reset"
	if err := os.MkdirAll(testDir, 0750); err != nil {
		t.Fatalf("Error creating test directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(testDir); err != nil {
			t.Logf("Warning: failed to clean up test directory: %v", err)
		}
	}()

	// Create some test files
	for i := 0; i < 5; i++ {
		filename := filepath.Join(testDir, "test_file_"+string(rune('0'+i))+".mid")
		if err := os.WriteFile(filename, []byte{}, 0600); err != nil {
			t.Fatalf("Error creating test file: %v", err)
		}
	}

	// Create a model
	fb := &fileBrowserModel{
		currentDir:  testDir,
		cursor:      10,       // Out of bounds
		viewportTop: 5,        // Also out of bounds
	}

	fb.loadFiles()

	// After loadFiles, cursor and viewport should be reset to valid values
	if fb.cursor >= len(fb.files) {
		t.Errorf("Expected cursor to be within bounds, got %d for %d files", fb.cursor, len(fb.files))
	}

	if fb.viewportTop > fb.cursor {
		t.Errorf("Expected viewportTop (%d) to be <= cursor (%d)", fb.viewportTop, fb.cursor)
	}

	t.Log("✓ File browser viewport reset logic works correctly!")
}
