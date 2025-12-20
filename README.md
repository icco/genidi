# genidi

A TUI (Terminal User Interface) MIDI generator and sequencer built with [Bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- **File Browser UI**: Navigate your filesystem to manage MIDI files
- **MIDI Sequencer**: 16-step sequencer with 4 channels
- **Visual Feedback**: Real-time clock visualization and step highlighting
- **Easy Editing**:
  - Toggle steps on/off for each channel
  - Adjust BPM (tempo) in real-time
  - Change MIDI notes per channel
  - Create and open MIDI files
- **Keyboard-driven**: Fully navigable with keyboard shortcuts

## Installation

```bash
go install github.com/icco/genidi@latest
```

Or build from source:

```bash
git clone https://github.com/icco/genidi
cd genidi
go build -o genidi
```

## Usage

genidi supports multiple modes of operation through subcommands.

### Manual Mode

Start the manual MIDI sequencer with an interactive TUI:

```bash
./genidi manual
```

To see all available commands:

```bash
./genidi --help
```

### File Browser Mode

Navigate and manage your MIDI files:

- `↑/k`: Move cursor up
- `↓/j`: Move cursor down
- `Enter`: Open directory or MIDI file
- `n`: Create a new MIDI file
- `q`: Quit application

### Sequencer Mode

Edit your MIDI sequence:

- `↑↓←→` or `hjkl`: Navigate the sequencer grid
- `Space`: Toggle step on/off
- `+/-`: Increase/decrease BPM (tempo)
- `w/s`: Increase/decrease MIDI note for current channel
- `p`: Play/stop (visual playback)
- `c`: Clear all steps in current channel
- `q`: Return to file browser

## Architecture

- **main.go**: Application entry point
- **cmd/**: Command-line interface using Cobra
  - **root.go**: Root command definition
  - **manual.go**: Manual mode command
- **internal/tui/**: TUI implementation
  - **model.go**: Core application state and file browser implementation
  - **sequencer.go**: MIDI sequencer logic and visualization

## MIDI Format

Generated MIDI files use:
- SMF (Standard MIDI File) format
- 16 steps per sequence
- 4 channels for different instruments/notes
- Configurable BPM (20-300)
- Note range: 0-127 (full MIDI range)

## Dependencies

- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style and layout
- [gomidi](https://gitlab.com/gomidi/midi) - MIDI file handling
- [Cobra](https://github.com/spf13/cobra) - CLI framework

## License

See LICENSE file for details.
