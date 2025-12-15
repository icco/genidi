package main

import (
"fmt"
"testing"
)

func TestMIDILoadingSavingWithPerStepNotes(t *testing.T) {
// Create a test MIDI file
testPath := "test_midi/test_load.mid"

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
