package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

const (
	numSteps    = 16
	numChannels = 4
	ticksPerQuarterNote = 960 // Standard MIDI resolution
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
}

func (s *sequencerModel) createNewMIDI(path string) error {
	s.filePath = path
	s.bpm = 120
	s.cursorX = 0
	s.cursorY = 0
	s.isPlaying = false
	s.currentStep = 0
	s.message = "New MIDI file created"

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
	s.message = fmt.Sprintf("Loaded: %s", path)

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

	switch msg.String() {
	case "left", "h":
		if s.cursorX > 0 {
			s.cursorX--
		}
	case "right", "l":
		if s.cursorX < numSteps-1 {
			s.cursorX++
		}
	case "up", "k":
		if s.cursorY > 0 {
			s.cursorY--
		}
	case "down", "j":
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
		// Toggle playback (visual only for now)
		s.isPlaying = !s.isPlaying
		if s.isPlaying {
			s.currentStep = 0
			return m, tick()
		}
	case "c":
		// Clear all steps in current channel
		for i := 0; i < numSteps; i++ {
			s.steps[s.cursorY][i] = false
		}
		if err := s.saveMIDI(); err != nil {
			s.message = fmt.Sprintf("Error saving: %v", err)
		}
	}

	return m, nil
}

func tick() tea.Cmd {
	// Tick faster for smoother visual feedback (60ms provides good balance)
	return tea.Tick(time.Millisecond*60, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) viewSequencer() string {
	s := m.sequencer

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("MIDI Sequencer Editor") + "\n\n")
	b.WriteString(fmt.Sprintf("File: %s\n", s.filePath))
	b.WriteString(fmt.Sprintf("BPM: %d (use +/- to adjust)\n\n", s.bpm))

	// Clock visualization
	clockBar := renderClockBar(s.bpm, s.isPlaying, s.currentStep)
	b.WriteString(clockBar + "\n\n")

	// Header row with proper spacing
	b.WriteString("Channel  Note  ")
	for i := 0; i < numSteps; i++ {
		b.WriteString(fmt.Sprintf("%2d ", i+1))
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

		// Steps (3 chars wide per step: " X ")
		for step := 0; step < numSteps; step++ {
			// Determine cell content
			var cell string
			if s.steps[ch][step] {
				cell = "●"
			} else {
				cell = "·"
			}

			// Apply styling
			cellStyle := lipgloss.NewStyle()
			
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

			b.WriteString(cellStyle.Render(fmt.Sprintf(" %s ", cell)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if s.message != "" {
		b.WriteString(errorStyle.Render(s.message) + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("Navigation: ↑↓←→ or hjkl • Space: toggle step • w/s: change note (for current step)"))
	b.WriteString("\n" + helpStyle.Render("+/-: tempo • p: play/stop • c: clear channel • q: back to files"))

	return b.String()
}

func renderClockBar(bpm int, isPlaying bool, currentStep int) string {
	barWidth := 50
	
	// Calculate position based on current step
	progress := float64(currentStep) / float64(numSteps)
	filled := int(progress * float64(barWidth))
	
	bar := strings.Builder{}
	bar.WriteString("Clock: [")
	
	for i := 0; i < barWidth; i++ {
		if i < filled && isPlaying {
			bar.WriteString("█")
		} else if i == filled && isPlaying {
			bar.WriteString("▶")
		} else {
			bar.WriteString("─")
		}
	}
	
	bar.WriteString("]")
	
	status := "Stopped"
	if isPlaying {
		status = "Playing"
	}
	
	return fmt.Sprintf("%s %s", bar.String(), status)
}

func midiNoteToName(note int) string {
	notes := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave := (note / 12) - 1
	noteName := notes[note%12]
	return fmt.Sprintf("%s%d", noteName, octave)
}
