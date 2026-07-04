package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// View modes
type viewMode int

const (
	fileBrowserMode viewMode = iota
	sequencerMode
)

// Key constants to avoid goconst linting issues
const (
	keyUp    = "up"
	keyDown  = "down"
	keyLeft  = "left"
	keyRight = "right"
)

// tickMsg drives playback; gen lets stale ticks from a previous start be dropped.
type tickMsg struct {
	gen int
}

// Model represents the application state
type model struct {
	mode        viewMode
	fileBrowser fileBrowserModel
	sequencer   sequencerModel
	width       int
	height      int
}

// fileBrowserModel manages the file browser state
type fileBrowserModel struct {
	currentDir  string
	files       []fileInfo
	cursor      int
	message     string
	viewportTop int // First visible file index
}

type fileInfo struct {
	name  string
	path  string
	isDir bool
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	dirStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00AAFF")).
			Bold(true)

	midiStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00"))
)

func InitialModel() model {
	currentDir, err := os.Getwd()
	if err != nil {
		currentDir = "."
	}

	fb := fileBrowserModel{
		currentDir: currentDir,
		cursor:     0,
	}
	fb.loadFiles()

	return model{
		mode:        fileBrowserMode,
		fileBrowser: fb,
		sequencer:   sequencerModel{},
	}
}

func (fb *fileBrowserModel) loadFiles() {
	fb.files = []fileInfo{}

	// Add parent directory entry
	if fb.currentDir != "/" {
		fb.files = append(fb.files, fileInfo{
			name:  "..",
			path:  filepath.Dir(fb.currentDir),
			isDir: true,
		})
	}

	entries, err := os.ReadDir(fb.currentDir)
	if err != nil {
		fb.message = fmt.Sprintf("Error reading directory: %v", err)
		return
	}

	for _, entry := range entries {
		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Include directories and MIDI files
		if entry.IsDir() || strings.HasSuffix(strings.ToLower(entry.Name()), ".mid") {
			fb.files = append(fb.files, fileInfo{
				name:  entry.Name(),
				path:  filepath.Join(fb.currentDir, entry.Name()),
				isDir: entry.IsDir(),
			})
		}
	}

	fb.adjustViewportBounds()
}

// adjustViewportBounds ensures cursor and viewport are within valid ranges
func (fb *fileBrowserModel) adjustViewportBounds() {
	// Reset cursor if out of bounds
	if fb.cursor >= len(fb.files) && len(fb.files) > 0 {
		fb.cursor = len(fb.files) - 1
	}
	if fb.cursor < 0 {
		fb.cursor = 0
	}
	// Ensure viewport top doesn't exceed cursor
	if fb.viewportTop > fb.cursor {
		fb.viewportTop = fb.cursor
	}
}

// clampViewport keeps the cursor visible within maxVisible rows.
func (fb *fileBrowserModel) clampViewport(maxVisible int) {
	if fb.cursor >= fb.viewportTop+maxVisible {
		fb.viewportTop = fb.cursor - maxVisible + 1
	}
	if fb.viewportTop > fb.cursor {
		fb.viewportTop = fb.cursor
	}
	if fb.viewportTop < 0 {
		fb.viewportTop = 0
	}
}

// maxVisibleLines is the number of file rows that fit after 9 lines of chrome.
func (m model) maxVisibleLines() int {
	lines := m.height - 9
	if lines < 5 {
		lines = 5
	}
	return lines
}

// uniqueSequencePath returns a new-sequence path in dir that doesn't exist yet.
func uniqueSequencePath(dir string) string {
	path := filepath.Join(dir, "new_sequence.mid")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("new_sequence_%d.mid", i))
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.fileBrowser.clampViewport(m.maxVisibleLines())
		return m, nil

	case tickMsg:
		// Only process ticks for the current chain; a stale tick doubles speed
		if m.sequencer.isPlaying && m.mode == sequencerMode && msg.gen == m.sequencer.tickGen {
			// Save previous step and advance immediately (so visual updates as early as possible)
			prevStep := m.sequencer.currentStep
			m.sequencer.currentStep = (m.sequencer.currentStep + 1) % numSteps
			currentStep := m.sequencer.currentStep

			// Send note offs for previous step's notes (they've been playing since last tick)
			for ch := 0; ch < numChannels; ch++ {
				if m.sequencer.steps[ch][prevStep] {
					// Safe cast: ch is bounded by numChannels (4), notes[ch][prevStep] is bounded by MIDI note range (0-127)
					m.sequencer.sendNoteOff(uint8(ch), uint8(m.sequencer.notes[ch][prevStep])) //nolint:gosec
				}
			}

			// Send note ons for current step's active notes
			for ch := 0; ch < numChannels; ch++ {
				if m.sequencer.steps[ch][currentStep] {
					// Safe cast: ch is bounded by numChannels (4), notes[ch][currentStep] is bounded by MIDI note range (0-127)
					m.sequencer.sendNoteOn(uint8(ch), uint8(m.sequencer.notes[ch][currentStep]), 100) //nolint:gosec
				}
			}

			// Schedule next tick
			return m, tickWithBPM(m.sequencer.bpm, msg.gen)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			// Close MIDI port before quitting
			m.sequencer.closePort()
			return m, tea.Quit
		case "q":
			if m.mode == fileBrowserMode {
				// Close MIDI port before quitting
				m.sequencer.closePort()
				return m, tea.Quit
			} else if !m.sequencer.selectingPort {
				// Return to file browser from sequencer
				m.mode = fileBrowserMode
				if m.sequencer.isPlaying {
					m.sequencer.isPlaying = false
					m.sequencer.stopPlayback()
				} else {
					m.sequencer.sendAllNotesOff()
				}
				return m, nil
			}
		}

		// Route to appropriate mode handler
		switch m.mode {
		case fileBrowserMode:
			return m.updateFileBrowser(msg)
		case sequencerMode:
			return m.updateSequencer(msg)
		}
	}

	return m, nil
}

func (m model) updateFileBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fb := &m.fileBrowser

	switch msg.String() {
	case keyUp, "k":
		if fb.cursor > 0 {
			fb.cursor--
			// Scroll up if cursor moves above viewport
			if fb.cursor < fb.viewportTop {
				fb.viewportTop = fb.cursor
			}
		}
	case keyDown, "j":
		if fb.cursor < len(fb.files)-1 {
			fb.cursor++
			fb.clampViewport(m.maxVisibleLines())
		}
	case "enter":
		if len(fb.files) == 0 {
			return m, nil
		}

		selected := fb.files[fb.cursor]
		if selected.isDir {
			fb.currentDir = selected.path
			fb.cursor = 0
			fb.viewportTop = 0
			fb.message = ""
			fb.loadFiles()
		} else {
			// Open MIDI file in sequencer
			err := m.sequencer.loadMIDI(selected.path)
			if err != nil {
				fb.message = fmt.Sprintf("Error loading MIDI: %v", err)
			} else {
				m.mode = sequencerMode
			}
		}
	case "n":
		// Create new MIDI file
		newPath := uniqueSequencePath(fb.currentDir)
		err := m.sequencer.createNewMIDI(newPath)
		if err != nil {
			fb.message = fmt.Sprintf("Error creating MIDI: %v", err)
		} else {
			m.mode = sequencerMode
		}
	}

	return m, nil
}

func (m model) View() string {
	switch m.mode {
	case fileBrowserMode:
		return m.viewFileBrowser()
	case sequencerMode:
		return m.viewSequencer()
	default:
		return "Unknown mode"
	}
}

func (m model) viewFileBrowser() string {
	fb := m.fileBrowser

	s := titleStyle.Render("GENIDI - MIDI Generator") + "\n\n"
	s += fmt.Sprintf("Current Directory: %s\n\n", fb.currentDir)

	if len(fb.files) == 0 {
		s += "No MIDI files or directories found.\n"
	} else {
		// Calculate viewport range
		start := fb.viewportTop
		end := start + m.maxVisibleLines()
		if end > len(fb.files) {
			end = len(fb.files)
		}

		// Render only visible files
		for i := start; i < end; i++ {
			file := fb.files[i]
			cursor := " "
			if i == fb.cursor {
				cursor = ">"
			}

			name := file.name
			if file.isDir {
				name = dirStyle.Render(name + "/")
			} else {
				name = midiStyle.Render(name)
			}

			// Format line consistently regardless of selection
			line := fmt.Sprintf("%s %s", cursor, name)
			if i == fb.cursor {
				s += selectedStyle.Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
	}

	s += "\n"
	if fb.message != "" {
		s += errorStyle.Render(fb.message) + "\n"
	}

	s += "\n" + helpStyle.Render("↑/k: up • ↓/j: down • enter: open • n: new MIDI • q: quit")

	return s
}
