package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icco/genidi/internal/audio"
	"github.com/spf13/cobra"
	"gitlab.com/gomidi/midi/v2/drivers"
	"gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

var (
	deviceName string
)

var virtualCmd = &cobra.Command{
	Use:   "virtual",
	Short: "Create a virtual MIDI device with audio output",
	Long: `Create a virtual MIDI input device that can receive MIDI commands from other applications.

The virtual device will show up as a MIDI output destination in other music software.
Any MIDI notes received will be played through the system audio output using a built-in synthesizer.

Example:
  genidi virtual --name "My Synth"
`,
	Run: runVirtual,
}

func init() {
	virtualCmd.Flags().StringVarP(&deviceName, "name", "n", "Genidi Virtual Synth", "Name for the virtual MIDI device")
	rootCmd.AddCommand(virtualCmd)
}

func runVirtual(cmd *cobra.Command, args []string) {
	p := tea.NewProgram(newVirtualModel(deviceName), tea.WithAltScreen())

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		p.Send(tea.Quit())
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}

// virtualModel represents the TUI state for the virtual MIDI device
type virtualModel struct {
	deviceName   string
	synth        *audio.Synth
	driver       *rtmididrv.Driver
	inPort       drivers.In
	stopFunc     func()
	activeNotes  map[string]noteDisplay // channel:note -> display info
	lastMessage  string
	messageCount int
	volume       float64
	err          error
	width        int
	height       int
}

type noteDisplay struct {
	channel  uint8
	note     uint8
	velocity uint8
	name     string
}

// midiEventMsg is sent when a MIDI message is received
type midiEventMsg struct {
	msgType  string
	channel  uint8
	note     uint8
	velocity uint8
}

func newVirtualModel(name string) *virtualModel {
	return &virtualModel{
		deviceName:  name,
		activeNotes: make(map[string]noteDisplay),
		volume:      0.5,
	}
}

func (m *virtualModel) Init() tea.Cmd {
	return m.initMIDI
}

func (m *virtualModel) initMIDI() tea.Msg {
	// Initialize the synthesizer
	synth, err := audio.NewSynth()
	if err != nil {
		return initResultMsg{err: fmt.Errorf("failed to initialize audio: %w", err)}
	}
	synth.SetVolume(0.5)

	// Create the rtmidi driver
	driver, err := rtmididrv.New()
	if err != nil {
		synth.Close()
		return initResultMsg{err: fmt.Errorf("failed to initialize MIDI driver: %w", err)}
	}

	// Create a virtual MIDI input port that other apps can send to
	inPort, err := driver.OpenVirtualIn(m.deviceName)
	if err != nil {
		driver.Close()
		synth.Close()
		return initResultMsg{err: fmt.Errorf("failed to create virtual MIDI port: %w", err)}
	}

	return initResultMsg{
		synth:  synth,
		driver: driver,
		inPort: inPort,
	}
}

type initResultMsg struct {
	synth  *audio.Synth
	driver *rtmididrv.Driver
	inPort drivers.In
	err    error
}

func (m *virtualModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case initResultMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.synth = msg.synth
		m.driver = msg.driver
		m.inPort = msg.inPort

		// Start listening for MIDI messages
		return m, m.listenMIDI

	case midiEventMsg:
		m.handleMIDIEvent(msg)
		m.messageCount++
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, m.cleanup
		case "up", "k":
			if m.volume < 1.0 {
				m.volume += 0.1
				if m.volume > 1.0 {
					m.volume = 1.0
				}
				if m.synth != nil {
					m.synth.SetVolume(m.volume)
				}
			}
		case "down", "j":
			if m.volume > 0 {
				m.volume -= 0.1
				if m.volume < 0 {
					m.volume = 0
				}
				if m.synth != nil {
					m.synth.SetVolume(m.volume)
				}
			}
		case " ":
			// Panic - all notes off
			if m.synth != nil {
				m.synth.AllNotesOff()
			}
			m.activeNotes = make(map[string]noteDisplay)
			m.lastMessage = "All notes off (panic)"
		}
	}

	return m, nil
}

func (m *virtualModel) listenMIDI() tea.Msg {
	if m.inPort == nil {
		return nil
	}

	// Use the driver's Listen method to receive MIDI messages
	stop, err := m.inPort.Listen(func(data []byte, timestamp int32) {
		if len(data) < 1 {
			return
		}

		status := data[0]
		msgType := status & 0xF0
		channel := status & 0x0F

		switch msgType {
		case 0x90: // Note On
			if len(data) >= 3 {
				note := data[1]
				velocity := data[2]
				if m.synth != nil {
					m.synth.NoteOn(channel, note, velocity)
				}
				m.activeNotes[fmt.Sprintf("%d:%d", channel, note)] = noteDisplay{
					channel:  channel,
					note:     note,
					velocity: velocity,
					name:     midiNoteName(note),
				}
				m.messageCount++
				if velocity > 0 {
					m.lastMessage = fmt.Sprintf("Note On: Ch%d %s vel:%d", channel+1, midiNoteName(note), velocity)
				} else {
					delete(m.activeNotes, fmt.Sprintf("%d:%d", channel, note))
					m.lastMessage = fmt.Sprintf("Note Off: Ch%d %s", channel+1, midiNoteName(note))
				}
			}
		case 0x80: // Note Off
			if len(data) >= 3 {
				note := data[1]
				if m.synth != nil {
					m.synth.NoteOff(channel, note)
				}
				delete(m.activeNotes, fmt.Sprintf("%d:%d", channel, note))
				m.messageCount++
				m.lastMessage = fmt.Sprintf("Note Off: Ch%d %s", channel+1, midiNoteName(note))
			}
		case 0xB0: // Control Change
			if len(data) >= 3 {
				controller := data[1]
				value := data[2]
				m.messageCount++
				m.lastMessage = fmt.Sprintf("CC: Ch%d ctrl:%d val:%d", channel+1, controller, value)
				// Handle all notes off (CC 123)
				if controller == 123 {
					if m.synth != nil {
						m.synth.AllNotesOff()
					}
					m.activeNotes = make(map[string]noteDisplay)
				}
			}
		case 0xE0: // Pitch Bend
			m.messageCount++
			m.lastMessage = fmt.Sprintf("Pitch Bend: Ch%d", channel+1)
		}
	}, drivers.ListenConfig{})

	if err != nil {
		m.err = fmt.Errorf("failed to listen to MIDI: %w", err)
	} else {
		m.stopFunc = stop
		m.lastMessage = fmt.Sprintf("Listening on: %s", m.inPort.String())
	}

	return nil
}

func (m *virtualModel) handleMIDIEvent(msg midiEventMsg) {
	key := fmt.Sprintf("%d:%d", msg.channel, msg.note)

	switch msg.msgType {
	case "noteOn":
		if msg.velocity > 0 {
			m.activeNotes[key] = noteDisplay{
				channel:  msg.channel,
				note:     msg.note,
				velocity: msg.velocity,
				name:     midiNoteName(msg.note),
			}
			m.lastMessage = fmt.Sprintf("Note On: Ch%d %s vel:%d",
				msg.channel+1, midiNoteName(msg.note), msg.velocity)
		} else {
			delete(m.activeNotes, key)
			m.lastMessage = fmt.Sprintf("Note Off: Ch%d %s",
				msg.channel+1, midiNoteName(msg.note))
		}
	case "noteOff":
		delete(m.activeNotes, key)
		m.lastMessage = fmt.Sprintf("Note Off: Ch%d %s",
			msg.channel+1, midiNoteName(msg.note))
	}
}

func (m *virtualModel) cleanup() tea.Msg {
	if m.stopFunc != nil {
		m.stopFunc()
	}
	if m.inPort != nil {
		m.inPort.Close()
	}
	if m.driver != nil {
		m.driver.Close()
	}
	if m.synth != nil {
		m.synth.AllNotesOff()
		m.synth.Close()
	}
	return tea.Quit()
}

func (m *virtualModel) View() string {
	var b strings.Builder

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF00")).
		Bold(true)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000")).
		Bold(true)

	noteStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFD700"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262"))

	// Title
	b.WriteString(titleStyle.Render("🎹 GENIDI Virtual MIDI Synth") + "\n\n")

	// Error display
	if m.err != nil {
		b.WriteString(errorStyle.Render("Error: "+m.err.Error()) + "\n\n")
		b.WriteString(helpStyle.Render("Press 'q' to quit"))
		return b.String()
	}

	// Device info
	b.WriteString(subtitleStyle.Render("Device Name: ") + m.deviceName + "\n")

	if m.inPort != nil {
		b.WriteString(subtitleStyle.Render("MIDI Port: ") + statusStyle.Render(m.inPort.String()) + "\n")
	} else {
		b.WriteString(subtitleStyle.Render("MIDI Port: ") + "Initializing...\n")
	}

	// Volume bar
	volumeBar := renderVolumeBar(m.volume)
	b.WriteString(subtitleStyle.Render("Volume: ") + volumeBar + "\n\n")

	// Status
	b.WriteString(statusStyle.Render("● Listening for MIDI") + "\n\n")

	// Active notes display
	b.WriteString(subtitleStyle.Render("Active Notes:") + "\n")
	if len(m.activeNotes) == 0 {
		b.WriteString("  (no notes playing)\n")
	} else {
		// Display active notes in a grid
		notesList := make([]string, 0, len(m.activeNotes))
		for _, nd := range m.activeNotes {
			notesList = append(notesList, fmt.Sprintf("Ch%d:%s", nd.channel+1, nd.name))
		}
		b.WriteString("  " + noteStyle.Render(strings.Join(notesList, " ")) + "\n")
	}

	// Last message
	b.WriteString("\n" + subtitleStyle.Render("Last Event: "))
	if m.lastMessage != "" {
		b.WriteString(m.lastMessage)
	} else {
		b.WriteString("(waiting for input)")
	}
	b.WriteString(fmt.Sprintf(" [%d total]\n", m.messageCount))

	// Keyboard visualization
	b.WriteString("\n" + renderKeyboard(m.activeNotes) + "\n")

	// Help
	b.WriteString("\n" + helpStyle.Render("↑/↓: volume • space: panic (all notes off) • q: quit"))

	return b.String()
}

func renderVolumeBar(vol float64) string {
	filled := int(vol * 20)
	empty := 20 - filled

	filledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))

	bar := filledStyle.Render(strings.Repeat("█", filled))
	bar += emptyStyle.Render(strings.Repeat("░", empty))
	bar += fmt.Sprintf(" %d%%", int(vol*100))

	return bar
}

func renderKeyboard(activeNotes map[string]noteDisplay) string {
	// Simple piano keyboard visualization (2 octaves around middle C)
	// Starting from C3 (48) to B4 (71) = 24 notes

	activeSet := make(map[uint8]bool)
	for _, nd := range activeNotes {
		activeSet[nd.note] = true
	}

	whiteStyle := lipgloss.NewStyle().Background(lipgloss.Color("#FFFFFF")).Foreground(lipgloss.Color("#000000"))
	blackStyle := lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Foreground(lipgloss.Color("#FFFFFF"))
	activeWhite := lipgloss.NewStyle().Background(lipgloss.Color("#00FF00")).Foreground(lipgloss.Color("#000000"))
	activeBlack := lipgloss.NewStyle().Background(lipgloss.Color("#00AA00")).Foreground(lipgloss.Color("#FFFFFF"))

	// White keys pattern: C D E F G A B
	// Black keys: C# D# _ F# G# A#
	var top, bottom strings.Builder

	for octave := 3; octave <= 4; octave++ {
		baseNote := uint8(octave*12 + 12) // C of this octave

		whiteKeys := []uint8{0, 2, 4, 5, 7, 9, 11}                        // C D E F G A B
		blackKeys := []int{1, 3, -1, 6, 8, 10}                            // C# D# _ F# G# A#
		blackPos := []bool{true, true, false, true, true, true, false}   // which white keys have black keys after

		// Top row (black keys)
		for i, hasBlack := range blackPos {
			if hasBlack && blackKeys[i] >= 0 {
				note := baseNote + uint8(blackKeys[i])
				if activeSet[note] {
					top.WriteString(activeBlack.Render("█"))
				} else {
					top.WriteString(blackStyle.Render("█"))
				}
			} else {
				top.WriteString(" ")
			}
			top.WriteString(" ")
		}

		// Bottom row (white keys)
		for _, offset := range whiteKeys {
			note := baseNote + offset
			if activeSet[note] {
				bottom.WriteString(activeWhite.Render("█"))
			} else {
				bottom.WriteString(whiteStyle.Render("█"))
			}
			bottom.WriteString(" ")
		}
	}

	return top.String() + "\n" + bottom.String()
}

func midiNoteName(note uint8) string {
	notes := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave := int(note/12) - 1
	noteName := notes[note%12]
	return fmt.Sprintf("%s%d", noteName, octave)
}

