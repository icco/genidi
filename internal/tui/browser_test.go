package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUniqueSequencePathAvoidsOverwriting(t *testing.T) {
	dir := t.TempDir()

	first := uniqueSequencePath(dir)
	if want := filepath.Join(dir, "new_sequence.mid"); first != want {
		t.Errorf("first path = %q, want %q", first, want)
	}

	if err := os.WriteFile(first, []byte{}, 0600); err != nil {
		t.Fatalf("creating file: %v", err)
	}

	second := uniqueSequencePath(dir)
	if second == first {
		t.Error("uniqueSequencePath returned an existing file's path")
	}
	if want := filepath.Join(dir, "new_sequence_2.mid"); second != want {
		t.Errorf("second path = %q, want %q", second, want)
	}
}

func TestStepIntervalPrecision(t *testing.T) {
	// 90 BPM → 166.666…ms per step; millisecond math would truncate to 166ms.
	if got, want := stepInterval(90), 166666666*time.Nanosecond; got != want {
		t.Errorf("stepInterval(90) = %v, want %v", got, want)
	}
}

func TestStepIntervalGuardsNonPositiveBPM(t *testing.T) {
	if got := stepInterval(0); got <= 0 {
		t.Errorf("stepInterval(0) = %v, want a positive duration", got)
	}
}
