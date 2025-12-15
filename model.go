package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// View modes
type viewMode int

const (
	fileBrowserMode viewMode = iota
	sequencerMode
)

// tickMsg is used for playback animation timing
type tickMsg time.Time

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
	currentDir string
	files      []fileInfo
	cursor     int
	message    string
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

func initialModel() model {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	fb := fileBrowserModel{
		currentDir: homeDir,
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

	// Reset cursor if out of bounds
	if fb.cursor >= len(fb.files) && len(fb.files) > 0 {
		fb.cursor = len(fb.files) - 1
	}
	if fb.cursor < 0 {
		fb.cursor = 0
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
		return m, nil

	case tickMsg:
		// Handle playback tick
		if m.sequencer.isPlaying {
			m.sequencer.currentStep = (m.sequencer.currentStep + 1) % numSteps
			return m, tick()
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.mode == fileBrowserMode {
				return m, tea.Quit
			} else {
				// Return to file browser from sequencer
				m.mode = fileBrowserMode
				m.sequencer.isPlaying = false
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
	case "up", "k":
		if fb.cursor > 0 {
			fb.cursor--
		}
	case "down", "j":
		if fb.cursor < len(fb.files)-1 {
			fb.cursor++
		}
	case "enter":
		if len(fb.files) == 0 {
			return m, nil
		}

		selected := fb.files[fb.cursor]
		if selected.isDir {
			fb.currentDir = selected.path
			fb.cursor = 0
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
		newPath := filepath.Join(fb.currentDir, "new_sequence.mid")
		err := m.sequencer.createNewMIDI(newPath)
		if err != nil {
			fb.message = fmt.Sprintf("Error creating MIDI: %v", err)
		} else {
			m.mode = sequencerMode
		}
	case "d":
		// Delete selected file
		if len(fb.files) > 0 {
			selected := fb.files[fb.cursor]
			if !selected.isDir && selected.name != ".." {
				err := os.Remove(selected.path)
				if err != nil {
					fb.message = fmt.Sprintf("Error deleting: %v", err)
				} else {
					fb.message = fmt.Sprintf("Deleted %s", selected.name)
					fb.loadFiles()
				}
			}
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
		for i, file := range fb.files {
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

			if i == fb.cursor {
				s += selectedStyle.Render(fmt.Sprintf("%s %s\n", cursor, name))
			} else {
				s += fmt.Sprintf("%s %s\n", cursor, name)
			}
		}
	}

	s += "\n"
	if fb.message != "" {
		s += errorStyle.Render(fb.message) + "\n"
	}

	s += "\n" + helpStyle.Render("↑/k: up • ↓/j: down • enter: open • n: new MIDI • d: delete • q: quit")

	return s
}
