package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/harmonica"
)

func TestMIDILoadingSavingWithPerStepNotes(t *testing.T) {
	// Create a test MIDI file
	testPath := "test_midi/test_load.mid"

	// Ensure the test directory exists
	if err := os.MkdirAll("test_midi", 0750); err != nil {
		t.Fatalf("Error creating test directory: %v", err)
	}
	// Clean up after test
	defer func() {
		if err := os.RemoveAll("test_midi"); err != nil {
			t.Logf("Warning: failed to clean up test directory: %v", err)
		}
	}()

	// Create the sequencer model and create a new MIDI file with some steps active
	s := &sequencerModel{}

	err := s.createNewMIDI(testPath)
	if err != nil {
		t.Fatalf("Error creating MIDI: %v", err)
	}

	// Set some steps with different notes
	s.steps[0][0] = true
	s.steps[0][4] = true
	s.steps[0][8] = true
	s.steps[0][12] = true
	s.notes[0][0] = 60  // C4
	s.notes[0][4] = 64  // E4
	s.notes[0][8] = 67  // G4
	s.notes[0][12] = 72 // C5

	s.steps[1][2] = true
	s.steps[1][6] = true
	s.steps[1][10] = true
	s.steps[1][14] = true
	s.notes[1][2] = 62  // D4
	s.notes[1][6] = 65  // F4
	s.notes[1][10] = 69 // A4
	s.notes[1][14] = 74 // D5

	err = s.saveMIDI()
	if err != nil {
		t.Fatalf("Error saving MIDI: %v", err)
	}

	// Now load it back and verify
	s2 := &sequencerModel{}
	err = s2.loadMIDI(testPath)
	if err != nil {
		t.Fatalf("Error loading MIDI: %v", err)
	}

	// Verify Channel 0 steps and notes
	tests := []struct {
		ch   int
		step int
		note int
		name string
	}{
		{0, 0, 60, "C4"},
		{0, 4, 64, "E4"},
		{0, 8, 67, "G4"},
		{0, 12, 72, "C5"},
		{1, 2, 62, "D4"},
		{1, 6, 65, "F4"},
		{1, 10, 69, "A4"},
		{1, 14, 74, "D5"},
	}

	for _, tt := range tests {
		if !s2.steps[tt.ch][tt.step] {
			t.Errorf("Expected step[%d][%d] to be active", tt.ch, tt.step)
		}
		if s2.notes[tt.ch][tt.step] != tt.note {
			t.Errorf("Expected note[%d][%d] = %d (%s), got %d",
				tt.ch, tt.step, tt.note, tt.name, s2.notes[tt.ch][tt.step])
		}
	}

	fmt.Println("✓ MIDI loading with per-step notes works correctly!")
}

func TestSignalVisualizer(t *testing.T) {
	// Create a sequencer with test data
	s := sequencerModel{
		bpm:         120,
		cursorX:     0,
		cursorY:     0,
		isPlaying:   false,
		currentStep: 0,
	}

	// Initialize spring for testing
	s.visualizerSpring = harmonica.NewSpring(harmonica.FPS(60), 6.0, 0.5)

	// Set up some notes across channels to test visualization
	// Channel 0: ascending pattern
	s.steps[0][0] = true
	s.steps[0][4] = true
	s.steps[0][8] = true
	s.steps[0][12] = true
	s.notes[0][0] = 60  // C4
	s.notes[0][4] = 64  // E4
	s.notes[0][8] = 67  // G4
	s.notes[0][12] = 72 // C5

	// Channel 1: different pattern
	s.steps[1][2] = true
	s.steps[1][6] = true
	s.notes[1][2] = 55 // G3
	s.notes[1][6] = 62 // D4

	// Channel 2: single note
	s.steps[2][5] = true
	s.notes[2][5] = 48 // C3

	// Channel 3: high notes
	s.steps[3][1] = true
	s.steps[3][9] = true
	s.notes[3][1] = 84 // C6
	s.notes[3][9] = 96 // C7

	// Render the signal visualizer (pass pointer for animation state)
	output := renderSignalVisualizer(&s)

	// Basic validation: check that output contains expected elements
	if output == "" {
		t.Error("Signal visualizer output should not be empty")
	}

	// Should contain the title
	if !containsString(output, "Signal Visualizer") {
		t.Error("Output should contain 'Signal Visualizer' title")
	}

	// Should contain legend with channel information
	if !containsString(output, "Legend") {
		t.Error("Output should contain legend")
	}

	// Should contain channel indicators
	for i := 1; i <= 4; i++ {
		chLabel := fmt.Sprintf("Ch%d", i)
		if !containsString(output, chLabel) {
			t.Errorf("Output should contain channel label %s", chLabel)
		}
	}

	// Should contain graph borders
	if !containsString(output, "│") {
		t.Error("Output should contain vertical graph borders")
	}
	if !containsString(output, "└") || !containsString(output, "┘") {
		t.Error("Output should contain bottom graph borders")
	}

	fmt.Println("✓ Signal visualizer rendering works correctly!")
	// Print the actual output to verify visually
	fmt.Println("\n--- Signal Visualizer Output ---")
	fmt.Println(output)
	fmt.Println("--- End Output ---")
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
