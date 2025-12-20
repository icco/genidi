package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "genidi",
	Short: "A TUI MIDI generator and sequencer",
	Long: `genidi is a Terminal User Interface (TUI) MIDI generator and sequencer built with Bubbletea.

It provides a visual interface for creating and editing MIDI sequences with multiple channels
and step-based editing.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
