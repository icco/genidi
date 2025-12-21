package tui

import (
	"fmt"
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
	s.midiOuts = nil
	s.midiOutNames = nil

	outs := midi.GetOutPorts()
	for _, out := range outs {
		s.midiOuts = append(s.midiOuts, out)
		s.midiOutNames = append(s.midiOutNames, out.String())
	}

	// If we had a selected port that's no longer available, reset
	if s.selectedOut >= len(s.midiOuts) {
		s.selectedOut = -1
		s.closePort()
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

func (s *sequencerModel) createNewMIDI(path string) error {
	s.filePath = path
	s.bpm = 120
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
	s.bpm = 120
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
		// If file doesn't exist, create a new one
		return s.saveMIDI()
	}

	// Extract tempo if available
	tempoChanges := rd.TempoChanges()
	if len(tempoChanges) > 0 {
		s.bpm = int(tempoChanges[0].BPM)
	}

	// Parse tracks to extract note data
	// Calculate ticks per step (one bar = 4 beats = 16 steps)
	ticksPerStep := uint32(ticksPerQuarterNote / 4) // 240 ticks per step

	tracks := rd.Tracks
	// Skip track 0 (tempo track), process remaining tracks as channels
	for trackIdx := 1; trackIdx < len(tracks) && trackIdx <= numChannels; trackIdx++ {
		ch := trackIdx - 1 // Track 1 maps to channel 0, etc.
		track := tracks[trackIdx]

		// Parse messages in the track
		var currentTick uint32
		for _, msg := range track {
			currentTick += msg.Delta

			// Check if this is a note on message
			var channel, key, velocity uint8
			if msg.Message.GetNoteOn(&channel, &key, &velocity) {
				// Calculate which step this note belongs to
				step := int(currentTick / ticksPerStep)
				if step < numSteps && velocity > 0 {
					s.notes[ch][step] = int(key)
					s.steps[ch][step] = true
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

	// Track 0: Tempo track
	var track0 smf.Track
	track0.Add(0, smf.MetaMeter(4, 4))
	track0.Add(0, smf.MetaTempo(float64(s.bpm)))
	track0.Close(0)
	if err := sm.Add(track0); err != nil {
		return fmt.Errorf("error adding tempo track: %w", err)
	}

	// Create tracks for each channel
	for ch := 0; ch < numChannels; ch++ {
		var track smf.Track
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
		case "escape", "q", "o":
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
		if s.bpm < 300 {
			s.bpm += 5
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "-", "_":
		// Decrease BPM
		if s.bpm > 20 {
			s.bpm -= 5
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "w":
		// Increase note for current step
		if s.notes[s.cursorY][s.cursorX] < 127 {
			s.notes[s.cursorY][s.cursorX]++
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "s":
		// Decrease note for current step
		if s.notes[s.cursorY][s.cursorX] > 0 {
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
			return m, tickWithBPM(s.bpm)
		} else {
			// Stop all notes when stopping playback
			s.sendAllNotesOff()
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

func tickWithBPM(bpm int) tea.Cmd {
	// Calculate step interval based on BPM
	// BPM = beats per minute, 16 steps = 4 beats (16th notes)
	// So each step = (60000ms / BPM) / 4
	stepIntervalMs := 60000 / bpm / 4
	return tea.Tick(time.Millisecond*time.Duration(stepIntervalMs), func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) viewSequencer() string {
	s := m.sequencer

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("MIDI Sequencer Editor") + "\n\n")
	b.WriteString(fmt.Sprintf("File: %s\n", s.filePath))
	b.WriteString(fmt.Sprintf("BPM: %d (use +/- to adjust)\n", s.bpm))

	// MIDI output status
	if s.outPort != nil {
		b.WriteString(fmt.Sprintf("MIDI Out: %s ✓\n\n", s.outPort.String()))
	} else {
		b.WriteString("MIDI Out: Not connected (press 'o' to select)\n\n")
	}

	// Port selection overlay
	if s.selectingPort {
		return m.viewPortSelection()
	}

	// Clock visualization
	clockBar := renderClockBar(s.bpm, s.isPlaying, s.currentStep)
	b.WriteString(clockBar + "\n\n")

	// Header row with proper spacing
	// 14 chars to match data rows: 8 for channel + 6 for note
	b.WriteString("Chan    Note  ")
	hexDigits := "0123456789ABCDEF"
	for i := 0; i < numSteps; i++ {
		b.WriteString(fmt.Sprintf(" %c ", hexDigits[i]))
	}
	b.WriteString("\n")

	// Sequencer grid
	for ch := 0; ch < numChannels; ch++ {
		// Channel indicator (8 chars wide to match "Channel  ")
		if ch == s.cursorY {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("Ch %-5d", ch+1)))
		} else {
			b.WriteString(fmt.Sprintf("Ch %-5d", ch+1))
		}

		// Note display for current cursor position (5 chars wide to match "Note  ")
		noteName := midiNoteToName(s.notes[ch][s.cursorX])
		if ch == s.cursorY {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("%-5s ", noteName)))
		} else {
			b.WriteString(fmt.Sprintf("%-5s ", noteName))
		}

		// Steps (3 chars wide per step)
		for step := 0; step < numSteps; step++ {
			// Determine cell content
			var cell string
			if s.steps[ch][step] {
				cell = " ● "
			} else {
				cell = " · "
			}

			// Apply styling with fixed width
			cellStyle := lipgloss.NewStyle().Width(3)

			// Highlight current cursor position
			if ch == s.cursorY && step == s.cursorX {
				cellStyle = cellStyle.Background(lipgloss.Color("#7D56F4"))
			}

			// Highlight playing step
			if s.isPlaying && step == s.currentStep {
				cellStyle = cellStyle.Foreground(lipgloss.Color("#00FF00")).Bold(true)
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
				b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, name, connected))
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

func renderClockBar(bpm int, isPlaying bool, currentStep int) string {
	// Colors for the clock bar - gradient from cyan to magenta
	colors := []string{
		"#00FFFF", "#00E5FF", "#00CCFF", "#00B2FF",
		"#0099FF", "#0080FF", "#0066FF", "#1A4DFF",
		"#3333FF", "#4D1AFF", "#6600FF", "#8000FF",
		"#9900FF", "#B300FF", "#CC00FF", "#FF00FF",
	}

	bar := strings.Builder{}
	// 14 chars to align with grid: "Chan    Note  " = 8 + 6 = 14 chars
	bar.WriteString("Clock         ")

	// Each step is 3 characters wide to match the grid
	for i := 0; i < numSteps; i++ {
		var cell string
		var cellStyle lipgloss.Style

		if isPlaying && i == currentStep {
			// Current playing position - bright indicator
			cell = " ▶ "
			cellStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color(colors[i])).
				Bold(true)
		} else if isPlaying && i < currentStep {
			// Already played - filled with color
			cell = " █ "
			cellStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colors[i]))
		} else {
			// Not yet played or stopped - dim
			cell = " · "
			cellStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#444444"))
		}

		bar.WriteString(cellStyle.Render(cell))
	}

	// Status after the bar
	status := " Stopped"
	if isPlaying {
		status = " Playing"
	}
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	if isPlaying {
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	}
	bar.WriteString(statusStyle.Render(status))

	return bar.String()
}

func midiNoteToName(note int) string {
	notes := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave := (note / 12) - 1
	noteName := notes[note%12]
	return fmt.Sprintf("%s%d", noteName, octave)
}
