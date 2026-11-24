package wine

import (
	"os"
	"runtime"
	"testing"
)

func TestRunning(t *testing.T) {
	if runtime.GOOS != "windows" {
		if Running() {
			t.Error("Running() is true off Windows")
		}
		return
	}
	// scripts/wine-exec.sh sets WINEPREFIX for the program it starts, and Windows never has it.
	_, underWine := os.LookupEnv("WINEPREFIX")
	if got := Running(); got != underWine {
		t.Errorf("Running() = %v, but WINEPREFIX is set: %v", got, underWine)
	}
}
