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
)

// sequencerModel manages the MIDI sequencer state
type sequencerModel struct {
	filePath    string
	bpm         int
	steps       [numChannels][numSteps]bool // Which steps are active
	notes       [numChannels]int            // MIDI note number for each channel
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

	// Initialize with default notes (C4, D4, E4, F4)
	s.notes = [numChannels]int{60, 62, 64, 65}

	// Clear all steps
	for i := 0; i < numChannels; i++ {
		for j := 0; j < numSteps; j++ {
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
	s.notes = [numChannels]int{60, 62, 64, 65}

	// Clear all steps
	for i := 0; i < numChannels; i++ {
		for j := 0; j < numSteps; j++ {
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

	return nil
}

func (s *sequencerModel) saveMIDI() error {
	if s.filePath == "" {
		return fmt.Errorf("no file path set")
	}

	// Create a new SMF file
	sm := smf.New()
	sm.TimeFormat = smf.MetricTicks(960)

	// Calculate ticks per step (one bar = 4 beats = 16 steps)
	ticksPerStep := uint32(960 / 4) // 240 ticks per step

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

		for step := 0; step < numSteps; step++ {
			if s.steps[ch][step] {
				pos := uint32(step) * ticksPerStep //nolint:gosec // step is bounded by numSteps constant
				// Note on
				track.Add(pos, midi.NoteOn(uint8(ch), uint8(s.notes[ch]), 100)) //nolint:gosec // ch is bounded by numChannels constant
				// Note off after one step
				track.Add(ticksPerStep-1, midi.NoteOff(uint8(ch), uint8(s.notes[ch]))) //nolint:gosec // ch is bounded by numChannels constant
			}
		}
		track.Close(uint32(numSteps) * ticksPerStep)
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
		// Increase note
		if s.notes[s.cursorY] < 127 {
			s.notes[s.cursorY]++
			if err := s.saveMIDI(); err != nil {
				s.message = fmt.Sprintf("Error saving: %v", err)
			}
		}
	case "s":
		// Decrease note
		if s.notes[s.cursorY] > 0 {
			s.notes[s.cursorY]--
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

	// Channel labels
	channelStyle := lipgloss.NewStyle().Width(10).Align(lipgloss.Left)
	b.WriteString(channelStyle.Render("Channel"))
	b.WriteString(channelStyle.Render("Note"))
	
	// Step numbers
	for i := 0; i < numSteps; i++ {
		b.WriteString(fmt.Sprintf("%2d ", i+1))
	}
	b.WriteString("\n")

	// Sequencer grid
	for ch := 0; ch < numChannels; ch++ {
		// Channel indicator
		if ch == s.cursorY {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("Ch %d     ", ch+1)))
		} else {
			b.WriteString(fmt.Sprintf("Ch %d     ", ch+1))
		}

		// Note display
		noteName := midiNoteToName(s.notes[ch])
		if ch == s.cursorY {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("%-4s ", noteName)))
		} else {
			b.WriteString(fmt.Sprintf("%-4s ", noteName))
		}

		// Steps
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

	b.WriteString("\n" + helpStyle.Render("Navigation: ↑↓←→ or hjkl • Space: toggle step • w/s: change note"))
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
