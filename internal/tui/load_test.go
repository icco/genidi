package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

func TestLoadMIDIReturnsErrorForCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.mid")
	original := []byte("this is not a midi file")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	s := &sequencerModel{}
	if err := s.loadMIDI(path); err == nil {
		t.Error("expected an error loading a corrupt MIDI file, got nil")
	}

	got, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatalf("reading file back: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Error("loading a corrupt MIDI file must not modify it")
	}
}

func TestLoadMIDIRespectsFileResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "res480.mid")

	sm := smf.New()
	sm.TimeFormat = smf.MetricTicks(480)
	var track smf.Track
	track.Add(0, smf.MetaTempo(120))
	// At 480 tpq a step is 120 ticks, so tick 480 is step 4.
	track.Add(480, midi.NoteOn(0, 72, 100))
	track.Add(100, midi.NoteOff(0, 72))
	track.Close(0)
	if err := sm.Add(track); err != nil {
		t.Fatalf("adding track: %v", err)
	}
	if err := sm.WriteFile(path); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	s := &sequencerModel{}
	if err := s.loadMIDI(path); err != nil {
		t.Fatalf("loading MIDI: %v", err)
	}

	if !s.steps[0][4] {
		t.Error("note at tick 480 in a 480-tpq file should land on step 4")
	}
	if s.steps[0][2] {
		t.Error("note landed on step 2: resolution hardcoded to 960 tpq instead of the file's")
	}
	if s.steps[0][4] && s.notes[0][4] != 72 {
		t.Errorf("step 4 note = %d, want 72", s.notes[0][4])
	}
}

func TestLoadMIDIClampsAndRoundsBPM(t *testing.T) {
	cases := []struct {
		tempo float64
		want  int
	}{
		{10, 20},     // below the sequencer's minimum
		{400, 300},   // above the sequencer's maximum
		{150.7, 151}, // fractional tempos round, not truncate
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("tempo_%v", tc.tempo), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tempo.mid")

			sm := smf.New()
			sm.TimeFormat = smf.MetricTicks(ticksPerQuarterNote)
			var track smf.Track
			track.Add(0, smf.MetaTempo(tc.tempo))
			track.Add(0, midi.NoteOn(0, 60, 100))
			track.Add(100, midi.NoteOff(0, 60))
			track.Close(0)
			if err := sm.Add(track); err != nil {
				t.Fatalf("adding track: %v", err)
			}
			if err := sm.WriteFile(path); err != nil {
				t.Fatalf("writing file: %v", err)
			}

			s := &sequencerModel{}
			if err := s.loadMIDI(path); err != nil {
				t.Fatalf("loading MIDI: %v", err)
			}
			if s.bpm != tc.want {
				t.Errorf("loaded bpm = %d, want %d", s.bpm, tc.want)
			}
		})
	}
}
