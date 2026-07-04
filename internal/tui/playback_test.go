package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pressKey(t *testing.T, m model, key string) (model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return nm.(model), cmd
}

func TestStaleTickIgnoredAfterRestart(t *testing.T) {
	m := model{mode: sequencerMode}
	m.sequencer.bpm = 120

	// Start playback: schedules the first tick chain.
	m, staleCmd := pressKey(t, m, "p")
	if staleCmd == nil {
		t.Fatal("expected a tick command when starting playback")
	}
	// Stop, then restart before the first tick fires.
	m, _ = pressKey(t, m, "p")
	m, freshCmd := pressKey(t, m, "p")
	if freshCmd == nil {
		t.Fatal("expected a tick command when restarting playback")
	}

	// The stale tick from the first start must not advance playback.
	nm, _ := m.Update(staleCmd())
	m = nm.(model)
	if got := m.sequencer.currentStep; got != 0 {
		t.Errorf("stale tick advanced playback: currentStep = %d, want 0", got)
	}

	// The fresh tick still drives playback.
	nm, _ = m.Update(freshCmd())
	m = nm.(model)
	if got := m.sequencer.currentStep; got != 1 {
		t.Errorf("fresh tick should advance playback: currentStep = %d, want 1", got)
	}
}

func TestEscCancelsPortSelection(t *testing.T) {
	m := model{mode: sequencerMode}
	m.sequencer.selectingPort = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = nm.(model)
	if m.sequencer.selectingPort {
		t.Error("esc should cancel port selection")
	}
}

func TestQuitSequencerStopsPlayback(t *testing.T) {
	m := model{mode: sequencerMode}
	m.sequencer.bpm = 120
	m.sequencer.isPlaying = true
	m.sequencer.currentStep = 5

	m, _ = pressKey(t, m, "q")
	if m.mode != fileBrowserMode {
		t.Error("q should return to the file browser")
	}
	if m.sequencer.isPlaying {
		t.Error("q should stop playback")
	}
	if m.sequencer.currentStep != 0 {
		t.Errorf("q should reset playback position: currentStep = %d, want 0", m.sequencer.currentStep)
	}
}

func TestResizeClampsViewport(t *testing.T) {
	m := model{mode: fileBrowserMode, height: 30}
	m.fileBrowser.files = make([]fileInfo, 30)
	m.fileBrowser.cursor = 20
	m.fileBrowser.viewportTop = 10

	// Shrinking to the 5-line minimum must scroll the viewport to the cursor.
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = nm.(model)

	wantTop := m.fileBrowser.cursor - 5 + 1
	if m.fileBrowser.viewportTop != wantTop {
		t.Errorf("viewportTop after shrink = %d, want %d (cursor must stay visible)", m.fileBrowser.viewportTop, wantTop)
	}
}
