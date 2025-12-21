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
	m := newVirtualModel(deviceName)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.program = p // Store reference so MIDI callback can send messages

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

const maxMessageHistory = 20

// virtualModel represents the TUI state for the virtual MIDI device
type virtualModel struct {
	deviceName     string
	synth          *audio.Synth
	driver         *rtmididrv.Driver
	inPort         drivers.In     // Single virtual MIDI input port (receives all channels)
	stopFunc       func()         // Stop function for the port
	activeNotes    map[string]noteDisplay // channel:note -> display info
	lastMessage    string
	messageHistory []string // Historical log of MIDI messages
	messageCount   int
	err            error
	width          int
	height         int
	program        *tea.Program // Reference to send messages from MIDI callback
}

type noteDisplay struct {
	channel  uint8
	note     uint8
	velocity uint8
	name     string
}

// midiEventMsg is sent when a MIDI message is received
type midiEventMsg struct {
	msgType    string
	channel    uint8
	note       uint8
	velocity   uint8
	controller uint8 // for CC messages
	value      uint8 // for CC messages
}

func newVirtualModel(name string) *virtualModel {
	return &virtualModel{
		deviceName:     name,
		activeNotes:    make(map[string]noteDisplay),
		messageHistory: make([]string, 0, maxMessageHistory),
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

	// Create the rtmidi driver
	driver, err := rtmididrv.New()
	if err != nil {
		synth.Close()
		return initResultMsg{err: fmt.Errorf("failed to initialize MIDI driver: %w", err)}
	}

	// Create a single virtual MIDI input port that receives all channels
	port, err := driver.OpenVirtualIn(m.deviceName)
	if err != nil {
		driver.Close()
		synth.Close()
		return initResultMsg{err: fmt.Errorf("failed to create virtual MIDI port: %w", err)}
	}

	return initResultMsg{
		synth:  synth,
		driver: driver,
		inPort: port,
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
		case "ctrl+c":
			return m, m.cleanup
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
		// Read the channel from the MIDI message (lower 4 bits of status byte)
		channel := status & 0x0F

		switch msgType {
		case 0x90: // Note On
			if len(data) >= 3 {
				note := data[1]
				velocity := data[2]
				// Play the note through synth
				if m.synth != nil {
					if velocity > 0 {
						m.synth.NoteOn(channel, note, velocity)
					} else {
						m.synth.NoteOff(channel, note)
					}
				}
				// Send message to update UI
				if m.program != nil {
					m.program.Send(midiEventMsg{
						msgType:  "noteOn",
						channel:  channel,
						note:     note,
						velocity: velocity,
					})
				}
			}
		case 0x80: // Note Off
			if len(data) >= 3 {
				note := data[1]
				if m.synth != nil {
					m.synth.NoteOff(channel, note)
				}
				// Send message to update UI
				if m.program != nil {
					m.program.Send(midiEventMsg{
						msgType: "noteOff",
						channel: channel,
						note:    note,
					})
				}
			}
		case 0xB0: // Control Change
			if len(data) >= 3 {
				controller := data[1]
				value := data[2]
				// Handle all notes off (CC 123)
				if controller == 123 && m.synth != nil {
					m.synth.AllNotesOff()
				}
				// Send message to update UI
				if m.program != nil {
					m.program.Send(midiEventMsg{
						msgType:    "cc",
						channel:    channel,
						controller: controller,
						value:      value,
					})
				}
			}
		case 0xE0: // Pitch Bend
			if m.program != nil {
				m.program.Send(midiEventMsg{
					msgType: "pitchBend",
					channel: channel,
				})
			}
		}
	}, drivers.ListenConfig{})

	if err != nil {
		m.err = fmt.Errorf("failed to listen to MIDI port: %w", err)
		return nil
	}

	m.stopFunc = stop
	m.lastMessage = fmt.Sprintf("Listening on: %s", m.inPort.String())
	return nil
}

func (m *virtualModel) handleMIDIEvent(msg midiEventMsg) {
	key := fmt.Sprintf("%d:%d", msg.channel, msg.note)
	var message string

	switch msg.msgType {
	case "noteOn":
		if msg.velocity > 0 {
			m.activeNotes[key] = noteDisplay{
				channel:  msg.channel,
				note:     msg.note,
				velocity: msg.velocity,
				name:     midiNoteName(msg.note),
			}
			message = fmt.Sprintf("Note On:  Ch%d %-4s vel:%d",
				msg.channel+1, midiNoteName(msg.note), msg.velocity)
		} else {
			delete(m.activeNotes, key)
			message = fmt.Sprintf("Note Off: Ch%d %-4s",
				msg.channel+1, midiNoteName(msg.note))
		}
	case "noteOff":
		delete(m.activeNotes, key)
		message = fmt.Sprintf("Note Off: Ch%d %-4s",
			msg.channel+1, midiNoteName(msg.note))
	case "cc":
		message = fmt.Sprintf("CC:       Ch%d ctrl:%d val:%d",
			msg.channel+1, msg.controller, msg.value)
		// Handle all notes off (CC 123)
		if msg.controller == 123 {
			m.activeNotes = make(map[string]noteDisplay)
		}
	case "pitchBend":
		message = fmt.Sprintf("Pitch Bend: Ch%d", msg.channel+1)
	}

	m.lastMessage = message

	// Add to history (keep most recent at top)
	if message != "" {
		m.messageHistory = append([]string{message}, m.messageHistory...)
		if len(m.messageHistory) > maxMessageHistory {
			m.messageHistory = m.messageHistory[:maxMessageHistory]
		}
	}
}

func (m *virtualModel) cleanup() tea.Msg {
	// Stop listener
	if m.stopFunc != nil {
		m.stopFunc()
	}
	// Close port
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
		b.WriteString(helpStyle.Render("Press Ctrl+C to quit"))
		return b.String()
	}

	// Device info
	b.WriteString(subtitleStyle.Render("Device Name: ") + m.deviceName + "\n")

	// Show MIDI port status
	if m.inPort != nil {
		b.WriteString(subtitleStyle.Render("MIDI Port: ") + statusStyle.Render(m.inPort.String()) + "\n")
		b.WriteString(subtitleStyle.Render("Channels: ") + "1-16 (reads channel from MIDI messages)\n\n")
	} else {
		b.WriteString(subtitleStyle.Render("MIDI Port: ") + "Initializing...\n\n")
	}

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

	// Message history log
	b.WriteString("\n" + subtitleStyle.Render(fmt.Sprintf("Message Log: [%d total]", m.messageCount)) + "\n")
	
	logStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	logHighlightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	
	if len(m.messageHistory) == 0 {
		b.WriteString("  " + logStyle.Render("(waiting for input)") + "\n")
	} else {
		// Show up to 10 most recent messages
		displayCount := len(m.messageHistory)
		if displayCount > 10 {
			displayCount = 10
		}
		for i := 0; i < displayCount; i++ {
			msg := m.messageHistory[i]
			if i == 0 {
				// Most recent message highlighted
				b.WriteString("  " + logHighlightStyle.Render("▶ "+msg) + "\n")
			} else {
				b.WriteString("  " + logStyle.Render("  "+msg) + "\n")
			}
		}
	}

	// Keyboard visualization
	b.WriteString("\n" + renderKeyboard(m.activeNotes) + "\n")

	// Help
	b.WriteString("\n" + helpStyle.Render("Ctrl+C: quit"))

	return b.String()
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

