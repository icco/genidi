package cmd

import "testing"

func TestCC123ClearsOnlyThatChannel(t *testing.T) {
	m := newVirtualModel("test")
	m.handleMIDIEvent(midiEventMsg{msgType: eventNoteOn, channel: 0, note: 60, velocity: 100})
	m.handleMIDIEvent(midiEventMsg{msgType: eventNoteOn, channel: 1, note: 62, velocity: 100})

	m.handleMIDIEvent(midiEventMsg{msgType: eventCC, channel: 0, controller: 123})

	if _, ok := m.activeNotes["0:60"]; ok {
		t.Error("CC 123 on channel 0 should clear channel 0's notes")
	}
	if _, ok := m.activeNotes["1:62"]; !ok {
		t.Error("CC 123 on channel 0 must not clear channel 1's notes (All Notes Off is per-channel)")
	}
}
