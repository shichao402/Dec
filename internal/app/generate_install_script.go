//go:build ignore

// Copies repo-root scripts/install.sh into embed/ for go:embed.
// Run: go generate ./internal/app
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	src := filepath.Join("..", "..", "scripts", "install.sh")
	dstDir := "embed"
	dst := filepath.Join(dstDir, "install.sh")

	data, err := os.ReadFile(src)
	if err != nil {
		fatal("read %s: %v", src, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		fatal("mkdir %s: %v", dstDir, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		fatal("write %s: %v", dst, err)
	}
	fmt.Printf("copied %s -> %s\n", src, dst)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
