package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	"gitlab.com/gomidi/midi/v2/smf"

	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

const (
	numSteps            = 16
	numChannels         = 4
	ticksPerQuarterNote = 960 // Standard MIDI resolution
	minMIDINote         = 0   // Minimum MIDI note value
	maxMIDINote         = 127 // Maximum MIDI note value
	notesPerOctave      = 12  // Number of notes in an octave
	minBPM              = 20
	maxBPM              = 300
	defaultBPM          = 120
)

// sequencerModel manages the MIDI sequencer state
type sequencerModel struct {
	filePath    string
	bpm         int
	steps       [numChannels][numSteps]bool // Which steps are active
	notes       [numChannels][numSteps]int  // MIDI note number for each step
	cursorX     int                         // Current step
	cursorY     int                         // Current channel
	isPlaying   bool
	currentStep int
	tickGen     int // current tick chain; stale ticks are dropped
	message     string

	// MIDI output
	midiOuts      []drivers.Out                // Available MIDI output ports
	midiOutNames  []string                     // Names of available ports
	selectedOut   int                          // Currently selected output index (-1 = none)
	outPort       drivers.Out                  // Currently open output port
	sendFunc      func(msg midi.Message) error // Function to send MIDI
	selectingPort bool                         // Whether we're in port selection mode

}

func (s *sequencerModel) refreshMIDIPorts() {
	// Remember the currently connected port name (if any)
	var connectedPortName string
	if s.outPort != nil {
		connectedPortName = s.outPort.String()
	}

	// Close existing connection - the port objects are about to be replaced
	s.closePort()
	s.selectedOut = -1

	s.midiOuts = nil
	s.midiOutNames = nil

	outs := midi.GetOutPorts()
	for _, out := range outs {
		s.midiOuts = append(s.midiOuts, out)
		s.midiOutNames = append(s.midiOutNames, out.String())
	}

	// If we had a connected port, try to reconnect to the same port by name
	if connectedPortName != "" {
		for i, name := range s.midiOutNames {
			if name == connectedPortName {
				if err := s.selectPort(i); err == nil {
					s.message = fmt.Sprintf("Reconnected to: %s", connectedPortName)
				}
				return
			}
		}
		// Port no longer available
		s.message = fmt.Sprintf("Port '%s' no longer available", connectedPortName)
	}
}

func (s *sequencerModel) selectPort(index int) error {
	if index < 0 || index >= len(s.midiOuts) {
		return fmt.Errorf("invalid port index")
	}

	// Close existing port if open
	s.closePort()

	// Open the new port
	out := s.midiOuts[index]
	send, err := midi.SendTo(out)
	if err != nil {
		return fmt.Errorf("failed to open port %s: %w", out.String(), err)
	}

	s.selectedOut = index
	s.outPort = out
	s.sendFunc = send
	s.message = fmt.Sprintf("Connected to: %s", out.String())
	return nil
}

func (s *sequencerModel) closePort() {
	if s.outPort != nil {
		// Send all notes off before closing
		if s.sendFunc != nil {
			for ch := 0; ch < numChannels; ch++ {
				// Safe cast: ch is bounded by numChannels constant (4)
				_ = s.sendFunc(midi.ControlChange(uint8(ch), 123, 0)) //nolint:gosec // All notes off
			}
		}
		_ = s.outPort.Close()
		s.outPort = nil
		s.sendFunc = nil
	}
}

func (s *sequencerModel) sendNoteOn(channel, note, velocity uint8) {
	if s.sendFunc != nil {
		_ = s.sendFunc(midi.NoteOn(channel, note, velocity))
	}
}

func (s *sequencerModel) sendNoteOff(channel, note uint8) {
	if s.sendFunc != nil {
		_ = s.sendFunc(midi.NoteOff(channel, note))
	}
}

func (s *sequencerModel) sendAllNotesOff() {
	if s.sendFunc != nil {
		for ch := 0; ch < numChannels; ch++ {
			// Safe cast: ch is bounded by numChannels constant (4)
			_ = s.sendFunc(midi.ControlChange(uint8(ch), 123, 0)) //nolint:gosec // All notes off
		}
	}
}

func (s *sequencerModel) stopPlayback() {
	if s.sendFunc != nil {
		// Send note offs for any notes that were playing on the current step
		for ch := 0; ch < numChannels; ch++ {
			if s.steps[ch][s.currentStep] {
				s.sendNoteOff(uint8(ch), uint8(s.notes[ch][s.currentStep])) //nolint:gosec
			}
		}
		// Send all notes off (CC#123) on all channels as a safety measure
		s.sendAllNotesOff()
		// Send MIDI Stop message (System Real-Time)
		if err := s.sendFunc(midi.Stop()); err != nil {
			s.message = fmt.Sprintf("Error sending MIDI stop: %v", err)
		}
	}
	// Reset playback position
	s.currentStep = 0
}

func (s *sequencerModel) createNewMIDI(path string) error {
	s.filePath = path
	s.bpm = defaultBPM
	s.cursorX = 0
	s.cursorY = 0
	s.isPlaying = false
	s.currentStep = 0
	s.selectedOut = -1
	s.selectingPort = false
	s.message = "New MIDI file created"

	// Refresh available MIDI ports
	s.refreshMIDIPorts()

	// Initialize with default notes (C4, D4, E4, F4) for each step
	defaultNotes := [numChannels]int{60, 62, 64, 65}
	for i := 0; i < numChannels; i++ {
		for j := 0; j < numSteps; j++ {
			s.notes[i][j] = defaultNotes[i] //nolint:gosec // i is bounded by numChannels constant
			s.steps[i][j] = false
		}
	}

	return s.saveMIDI()
}

func (s *sequencerModel) loadMIDI(path string) error {
	s.filePath = path
	s.bpm = defaultBPM
	s.cursorX = 0
	s.cursorY = 0
	s.isPlaying = false
	s.currentStep = 0
	s.selectedOut = -1
	s.selectingPort = false
	s.message = fmt.Sprintf("Loaded: %s", path)

	// Refresh available MIDI ports
	s.refreshMIDIPorts()

	// Initialize with default notes
	defaultNotes := [numChannels]int{60, 62, 64, 65}
	for i := 0; i < numChannels; i++ {
		for j := 0; j < numSteps; j++ {
			s.notes[i][j] = defaultNotes[i] //nolint:gosec // i is bounded by numChannels constant
			s.steps[i][j] = false
		}
	}

	// Try to parse existing MIDI file
	rd, err := smf.ReadFile(path)
	if err != nil {
		// Create a new file only if missing; never overwrite a corrupt file.
		if errors.Is(err, fs.ErrNotExist) {
			return s.saveMIDI()
		}
		return fmt.Errorf("error reading MIDI file: %w", err)
	}

	// Extract tempo if available
	tempoChanges := rd.TempoChanges()
	if len(tempoChanges) > 0 {
		s.bpm = clampBPM(tempoChanges[0].BPM)
	}

	// One step is a 16th note; use the file's resolution, not our default.
	ticksPerStep := uint32(ticksPerQuarterNote / 4)
	if mt, ok := rd.TimeFormat.(smf.MetricTicks); ok && mt.Ticks16th() > 0 {
		ticksPerStep = mt.Ticks16th()
	}

	// Map notes by each message's channel, not track position, so any
	// track layout works (format 0 or one track per channel).
	for _, track := range rd.Tracks {
		var currentTick uint32
		for _, msg := range track {
			currentTick += msg.Delta

			// Check if this is a note on message
			var channel, key, velocity uint8
			if msg.Message.GetNoteOn(&channel, &key, &velocity) {
				// Calculate which step this note belongs to
				step := int(currentTick / ticksPerStep)
				if int(channel) < numChannels && step < numSteps && velocity > 0 {
					s.notes[channel][step] = int(key)
					s.steps[channel][step] = true
				}
			}
		}
	}

	return nil
}

func (s *sequencerModel) saveMIDI() error {
	if s.filePath == "" {
		return fmt.Errorf("no file path set")
	}

	// Create a new SMF file
	sm := smf.New()
	sm.TimeFormat = smf.MetricTicks(ticksPerQuarterNote)

	// Calculate ticks per step (one bar = 4 beats = 16 steps)
	ticksPerStep := uint32(ticksPerQuarterNote / 4) // 240 ticks per step

	// Track 0: tempo track, named after the file so DAWs show a sequence name
	sequenceName := strings.TrimSuffix(filepath.Base(s.filePath), filepath.Ext(s.filePath))
	var track0 smf.Track
	track0.Add(0, smf.MetaTrackSequenceName(sequenceName))
	track0.Add(0, smf.MetaMeter(4, 4))
	track0.Add(0, smf.MetaTempo(float64(s.bpm)))
	track0.Close(0)
	if err := sm.Add(track0); err != nil {
		return fmt.Errorf("error adding tempo track: %w", err)
	}

	// Create tracks for each channel
	for ch := 0; ch < numChannels; ch++ {
		var track smf.Track
		track.Add(0, smf.MetaTrackSequenceName(fmt.Sprintf("Channel %d", ch+1)))
		var lastTick uint32 = 0

		for step := 0; step < numSteps; step++ {
			if s.steps[ch][step] {
				pos := uint32(step) * ticksPerStep //nolint:gosec // step is bounded by numSteps constant
				delta := pos - lastTick
				// Note on
				track.Add(delta, midi.NoteOn(uint8(ch), uint8(s.notes[ch][step]), 100)) //nolint:gosec // ch is bounded by numChannels constant
				lastTick = pos
				// Note off after one step
				track.Add(ticksPerStep-1, midi.NoteOff(uint8(ch), uint8(s.notes[ch][step]))) //nolint:gosec // ch is bounded by numChannels constant
				lastTick += ticksPerStep - 1
			}
		}
		// Close track - ensure we don't have negative delta
		endTick := uint32(numSteps) * ticksPerStep
		if lastTick < endTick {
			track.Close(endTick - lastTick)
		} else {
			track.Close(0)
		}
		if err := sm.Add(track); err != nil {
			return fmt.Errorf("error adding track %d: %w", ch, err)
		}
	}

	// Write to file
	err := sm.WriteFile(s.filePath)
	if err != nil {
		return fmt.Errorf("error writing MIDI file: %w", err)
	}

	s.message = "MIDI file saved"
	return nil
}

func (m model) updateSequencer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.sequencer

	// Handle port selection mode
	if s.selectingPort {
		switch msg.String() {
		case keyUp, "k":
			if s.selectedOut > 0 {
				s.selectedOut--
			} else if s.selectedOut == -1 && len(s.midiOuts) > 0 {
				s.selectedOut = 0
			}
		case keyDown, "j":
			if s.selectedOut < len(s.midiOuts)-1 {
				s.selectedOut++
			}
		case "enter":
			if s.selectedOut >= 0 && s.selectedOut < len(s.midiOuts) {
				if err := s.selectPort(s.selectedOut); err != nil {
					s.message = fmt.Sprintf("Error: %v", err)
				}
			}
			s.selectingPort = false
		case "esc", "q", "o":
			s.selectingPort = false
		case "r":
			// Refresh ports list
			s.refreshMIDIPorts()
			s.message = fmt.Sprintf("Found %d MIDI output(s)", len(s.midiOuts))
		}
		return m, nil
	}

	switch msg.String() {
	case keyLeft, "h":
		if s.cursorX > 0 {
			s.cursorX--
		}
	case keyRight, "l":
		if s.cursorX < numSteps-1 {
			s.cursorX++
		}
	case keyUp, "k":
		if s.cursorY > 0 {
			s.cursorY--
		}
	case keyDown, "j":
		if s.cursorY < numChannels-1 {
			s.cursorY++
		}
	case " ":
		// Toggle step
		s.steps[s.cursorY][s.cursorX] = !s.steps[s.cursorY][s.cursorX]
		if err := s.saveMIDI(); err != nil {
			s.message = fmt.Sprintf("Error saving: %v", err)
		}
	case "+", "=":
		// Increase BPM
		if s.bpm < maxBPM {
			s.bpm += 5
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "-", "_":
		// Decrease BPM
		if s.bpm > minBPM {
			s.bpm -= 5
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "w":
		// Increase note for current step
		if s.notes[s.cursorY][s.cursorX] < maxMIDINote {
			s.notes[s.cursorY][s.cursorX]++
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "s":
		// Decrease note for current step
		if s.notes[s.cursorY][s.cursorX] > minMIDINote {
			s.notes[s.cursorY][s.cursorX]--
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "p":
		// Toggle playback
		s.isPlaying = !s.isPlaying
		if s.isPlaying {
			s.currentStep = 0
			s.tickGen++ // invalidate in-flight ticks
			// Play notes at step 0 immediately
			for ch := 0; ch < numChannels; ch++ {
				if s.steps[ch][0] {
					s.sendNoteOn(uint8(ch), uint8(s.notes[ch][0]), 100) //nolint:gosec
				}
			}
			return m, tickWithBPM(s.bpm, s.tickGen)
		} else {
			// Stop playback - send note offs for currently playing step and reset state
			s.stopPlayback()
		}
	case "c":
		// Clear all steps in current channel
		for i := 0; i < numSteps; i++ {
			s.steps[s.cursorY][i] = false
		}
		if err := s.saveMIDI(); err != nil {
			s.message = fmt.Sprintf("Error saving: %v", err)
		}
	case "o":
		// Open MIDI output port selection
		s.refreshMIDIPorts()
		s.selectingPort = true
		if len(s.midiOuts) == 0 {
			s.message = "No MIDI outputs found. Press 'r' to refresh."
		} else {
			s.message = fmt.Sprintf("Found %d MIDI output(s)", len(s.midiOuts))
		}
	}

	return m, nil
}

// stepInterval returns the duration of one 16th-note step at the given tempo.
func stepInterval(bpm int) time.Duration {
	if bpm <= 0 {
		bpm = defaultBPM
	}
	// 16 steps = 4 beats, so each step is a quarter of a beat.
	return time.Minute / time.Duration(bpm) / 4
}

func tickWithBPM(bpm, gen int) tea.Cmd {
	return tea.Tick(stepInterval(bpm), func(time.Time) tea.Msg {
		return tickMsg{gen: gen}
	})
}

func (m model) viewSequencer() string {
	s := m.sequencer

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("MIDI Sequencer Editor") + "\n\n")
	fmt.Fprintf(&b, "File: %s\n", s.filePath)
	fmt.Fprintf(&b, "BPM: %d (use +/- to adjust)\n", s.bpm)

	// MIDI output status
	if s.outPort != nil {
		fmt.Fprintf(&b, "MIDI Out: %s ✓\n\n", s.outPort.String())
	} else {
		b.WriteString("MIDI Out: Not connected (press 'o' to select)\n\n")
	}

	// Port selection overlay
	if s.selectingPort {
		return m.viewPortSelection()
	}


	// Header row with proper spacing
	// 14 chars to match data rows: 8 for channel + 6 for note
	b.WriteString("Chan    Note  ")
	hexDigits := "0123456789ABCDEF"
	for i := 0; i < numSteps; i++ {
		headerStyle := lipgloss.NewStyle().Width(3).Align(lipgloss.Center).Foreground(lipgloss.Color("#888888"))
		// Highlight the currently playing column
		if s.isPlaying && i == s.currentStep {
			headerStyle = headerStyle.Background(lipgloss.Color("#7D56F4")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
		}
		b.WriteString(headerStyle.Render(string(hexDigits[i])))
	}
	b.WriteString("\n")

	// Sequencer grid
	for ch := 0; ch < numChannels; ch++ {
		// Channel indicator (8 chars wide to match "Channel  ")
		if ch == s.cursorY {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("Ch %-5d", ch+1)))
		} else {
			fmt.Fprintf(&b, "Ch %-5d", ch+1)
		}

		// Note display for current cursor position (5 chars wide to match "Note  ")
		noteName := midiNoteToName(s.notes[ch][s.cursorX])
		if ch == s.cursorY {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("%-5s ", noteName)))
		} else {
			fmt.Fprintf(&b, "%-5s ", noteName)
		}

		// Steps (3 chars wide per step)
		for step := 0; step < numSteps; step++ {
			// Determine cell content
			var cell string
			if s.steps[ch][step] {
				cell = "●"
			} else {
				cell = "·"
			}

			// Apply styling with fixed width and center alignment
			cellStyle := lipgloss.NewStyle().Width(3).Align(lipgloss.Center)

			// Highlight the currently playing column
			if s.isPlaying && step == s.currentStep {
				cellStyle = cellStyle.Background(lipgloss.Color("#7D56F4"))
			}

			// Highlight current cursor position (overrides playing column)
			if ch == s.cursorY && step == s.cursorX {
				cellStyle = cellStyle.Background(lipgloss.Color("#5A3DBF"))
			}

			// Active step gets color
			if s.steps[ch][step] {
				cellStyle = cellStyle.Foreground(lipgloss.Color("#FFD700"))
			} else {
				cellStyle = cellStyle.Foreground(lipgloss.Color("#666666"))
			}

			b.WriteString(cellStyle.Render(cell))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if s.message != "" {
		b.WriteString(errorStyle.Render(s.message) + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("Navigation: ↑↓←→ or hjkl • Space: toggle step • w/s: change note (for current step)"))
	b.WriteString("\n" + helpStyle.Render("+/-: tempo • p: play/stop • c: clear channel • o: MIDI output • q: back to files"))

	return b.String()
}

func (m model) viewPortSelection() string {
	s := m.sequencer

	var b strings.Builder

	b.WriteString(titleStyle.Render("Select MIDI Output") + "\n\n")

	if len(s.midiOutNames) == 0 {
		b.WriteString("No MIDI output ports found.\n\n")
		b.WriteString("Make sure your MIDI interface is connected.\n")
	} else {
		for i, name := range s.midiOutNames {
			cursor := "  "
			if i == s.selectedOut {
				cursor = "> "
			}

			// Mark currently connected port
			connected := ""
			if s.outPort != nil && s.outPort.String() == name {
				connected = " (connected)"
			}

			if i == s.selectedOut {
				b.WriteString(selectedStyle.Render(fmt.Sprintf("%s%s%s\n", cursor, name, connected)))
			} else {
				fmt.Fprintf(&b, "%s%s%s\n", cursor, name, connected)
			}
		}
	}

	b.WriteString("\n")
	if s.message != "" {
		b.WriteString(errorStyle.Render(s.message) + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("↑/k: up • ↓/j: down • enter: select • r: refresh • q/esc: cancel"))

	return b.String()
}

func midiNoteToName(note int) string {
	notes := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave := (note / notesPerOctave) - 1
	noteName := notes[note%notesPerOctave]
	return fmt.Sprintf("%s%d", noteName, octave)
}

// clampBPM clamps a file tempo to range; 0/Inf/NaN would break tick scheduling.
func clampBPM(bpm float64) int {
	switch {
	case !(bpm >= minBPM): // negated comparison also catches NaN
		return minBPM
	case bpm > maxBPM:
		return maxBPM
	default:
		return int(math.Round(bpm))
	}
}
