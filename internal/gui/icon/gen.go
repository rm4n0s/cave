//go:build ignore

// Command gen writes the PNG files of Cave's icon for the packaging, packaging/icons/cave-<size>.png,
// from package icon. Run it with `go generate ./internal/gui/icon`.
package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/rm4n0s/cave/internal/gui/icon"
)

func main() {
	dir := filepath.Join("..", "..", "..", "packaging", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	for _, size := range icon.Sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, icon.Draw(size)); err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
		name := filepath.Join(dir, fmt.Sprintf("cave-%d.png", size))
		if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
	}
}
