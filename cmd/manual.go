package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icco/genidi/internal/tui"
	"github.com/spf13/cobra"
)

var manualCmd = &cobra.Command{
	Use:   "manual",
	Short: "Start the manual MIDI sequencer",
	Long: `Start the manual MIDI sequencer with an interactive TUI interface.

This mode provides a file browser and sequencer interface for manually creating
and editing MIDI sequences step by step.`,
	Run: runManual,
}

func init() {
	rootCmd.AddCommand(manualCmd)
}

func runManual(cmd *cobra.Command, args []string) {
	p := tea.NewProgram(tui.InitialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
