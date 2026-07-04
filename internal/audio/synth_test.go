package audio

import "testing"

// newTestSynth builds a Synth without an audio device.
func newTestSynth(maxVoices int) *Synth {
	return &Synth{maxVoices: maxVoices, masterVolume: 0.3}
}

func activeVoices(s *Synth, channel, note uint8) []*Voice {
	var out []*Voice
	for _, v := range s.voices {
		if v != nil && v.active && v.channel == channel && v.note == note {
			out = append(out, v)
		}
	}
	return out
}

func TestNoteOnRetriggerReusesVoice(t *testing.T) {
	s := newTestSynth(8)
	s.NoteOn(0, 60, 100)
	s.NoteOn(0, 60, 110)

	if got := len(activeVoices(s, 0, 60)); got != 1 {
		t.Errorf("retriggered note should reuse its voice: got %d active voices, want 1", got)
	}
}

func TestNoteOffAfterRetriggerReleasesNote(t *testing.T) {
	s := newTestSynth(8)
	s.NoteOn(0, 60, 100)
	s.NoteOn(0, 60, 110)
	s.NoteOff(0, 60)

	for _, v := range activeVoices(s, 0, 60) {
		if !v.releasing {
			t.Error("all voices for a note should be releasing after NoteOff; found a stuck voice")
		}
	}
}

func TestVoiceStealingPrefersReleasingVoice(t *testing.T) {
	s := newTestSynth(2)
	s.NoteOn(0, 60, 100)
	s.NoteOn(0, 62, 100)
	s.NoteOff(0, 62) // 62 is releasing; 60 is still held
	s.NoteOn(0, 64, 100)

	held := activeVoices(s, 0, 60)
	if len(held) != 1 || held[0].releasing {
		t.Error("voice stealing should take the releasing voice, not a held note")
	}
	if len(activeVoices(s, 0, 64)) != 1 {
		t.Error("new note should be playing after stealing a voice")
	}
}

func TestReadReportsWrittenBytes(t *testing.T) {
	r := &synthReader{synth: newTestSynth(8)}
	buf := make([]byte, 7) // not a multiple of the 4-byte frame size

	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 4 {
		t.Errorf("Read wrote one 4-byte frame but reported n = %d, want 4", n)
	}
}

func TestCloseWithoutPlayerDoesNotPanic(t *testing.T) {
	s := newTestSynth(8)
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestChannelAllNotesOffReleasesOnlyThatChannel(t *testing.T) {
	s := newTestSynth(8)
	s.NoteOn(0, 60, 100)
	s.NoteOn(1, 62, 100)

	s.ChannelAllNotesOff(0)

	if vs := activeVoices(s, 0, 60); len(vs) != 1 || !vs[0].releasing {
		t.Error("channel 0's voice should be releasing after ChannelAllNotesOff(0)")
	}
	if vs := activeVoices(s, 1, 62); len(vs) != 1 || vs[0].releasing {
		t.Error("channel 1's voice must be unaffected by ChannelAllNotesOff(0)")
	}
}
