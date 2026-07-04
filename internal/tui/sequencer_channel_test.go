package tui

import (
	"path/filepath"
	"testing"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

// writeSingleTrackMIDI writes a format-0 style file: one track with tempo
// meta events plus one note per step on the given channels.
func writeSingleTrackMIDI(t *testing.T, path string, channels []uint8) {
	t.Helper()

	sm := smf.New()
	sm.TimeFormat = smf.MetricTicks(ticksPerQuarterNote)
	ticksPerStep := uint32(ticksPerQuarterNote / 4)

	var track smf.Track
	track.Add(0, smf.MetaMeter(4, 4))
	track.Add(0, smf.MetaTempo(120))
	for i, ch := range channels {
		var delta uint32
		if i > 0 {
			delta = 1 // 1 tick after the previous note-off
		}
		track.Add(delta, midi.NoteOn(ch, 60+ch, 100))
		track.Add(ticksPerStep-1, midi.NoteOff(ch, 60+ch))
	}
	track.Close(0)
	if err := sm.Add(track); err != nil {
		t.Fatalf("adding track: %v", err)
	}
	if err := sm.WriteFile(path); err != nil {
		t.Fatalf("writing file: %v", err)
	}
}

func TestLoadMIDIMapsNotesByChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "format0.mid")
	writeSingleTrackMIDI(t, path, []uint8{0, 1, 2, 3})

	s := &sequencerModel{}
	if err := s.loadMIDI(path); err != nil {
		t.Fatalf("loading MIDI: %v", err)
	}

	// The note on MIDI channel ch sits at step ch and must land on row ch.
	for ch := 0; ch < 4; ch++ {
		if !s.steps[ch][ch] {
			t.Errorf("note on MIDI channel %d did not land on row %d at step %d", ch, ch, ch)
		}
		if got, want := s.notes[ch][ch], 60+ch; got != want {
			t.Errorf("row %d step %d note = %d, want %d", ch, ch, got, want)
		}
	}
}

func TestLoadMIDIIgnoresOutOfRangeChannels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "highchannels.mid")
	writeSingleTrackMIDI(t, path, []uint8{0, 9, 15})

	s := &sequencerModel{}
	if err := s.loadMIDI(path); err != nil {
		t.Fatalf("loading MIDI: %v", err)
	}

	if !s.steps[0][0] {
		t.Errorf("note on channel 0 should still load onto row 0")
	}
	// Channels 9 and 15 don't fit the 4-row grid and must be dropped.
	for ch := 1; ch < numChannels; ch++ {
		for step := 0; step < numSteps; step++ {
			if s.steps[ch][step] {
				t.Errorf("unexpected active step at row %d step %d from out-of-range channel", ch, step)
			}
		}
	}
}

func TestSaveMIDIWritesTrackNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named.mid")

	s := &sequencerModel{}
	if err := s.createNewMIDI(path); err != nil {
		t.Fatalf("creating MIDI: %v", err)
	}
	s.steps[0][0] = true
	s.steps[2][4] = true
	if err := s.saveMIDI(); err != nil {
		t.Fatalf("saving MIDI: %v", err)
	}

	rd, err := smf.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file back: %v", err)
	}

	if len(rd.Tracks) != numChannels+1 {
		t.Fatalf("expected %d tracks, got %d", numChannels+1, len(rd.Tracks))
	}

	wantNames := []string{"named", "Channel 1", "Channel 2", "Channel 3", "Channel 4"}
	for i, track := range rd.Tracks {
		var name string
		found := false
		for _, ev := range track {
			if ev.Message.GetMetaTrackName(&name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("track %d has no track name event", i)
			continue
		}
		if name != wantNames[i] {
			t.Errorf("track %d name = %q, want %q", i, name, wantNames[i])
		}
	}
}
