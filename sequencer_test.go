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

	// Initialize spring for waveform animation (slower decay, less bouncy)
	s.waveformSpring = harmonica.NewSpring(harmonica.FPS(60), 3.0, 0.8)

	// Initialize waveform history (64 samples per channel)
	const historyLength = 64
	for i := 0; i < numChannels; i++ {
		s.waveformHistory[i] = make([]float64, historyLength)
		s.currentLevels[i] = 0
		s.levelVelocities[i] = 0
	}

	// Set up some notes across channels
	s.steps[0][0] = true
	s.steps[0][4] = true
	s.notes[0][0] = 60 // C4
	s.notes[0][4] = 64 // E4

	s.steps[1][2] = true
	s.notes[1][2] = 55 // G3

	// Simulate some waveform history with varying levels
	for x := 0; x < historyLength; x++ {
		// Channel 0: decaying signal
		if x > 50 {
			s.waveformHistory[0][x] = 0.8 * float64(historyLength-x) / float64(historyLength-50)
		}
		// Channel 1: spike in the middle
		if x > 30 && x < 40 {
			s.waveformHistory[1][x] = 0.6
		}
	}

	// Render the signal visualizer
	output := renderSignalVisualizer(&s)

	// Basic validation: check that output contains expected elements
	if output == "" {
		t.Error("Signal visualizer output should not be empty")
	}

	// Should contain the title
	if !containsString(output, "Signal Output") {
		t.Error("Output should contain 'Signal Output' title")
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

	// Should contain time indicators
	if !containsString(output, "past") || !containsString(output, "now") {
		t.Error("Output should contain time indicators")
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
