package app

import (
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/internal/types"
)

func repositoryHasLegacyLayout(repoDir string) bool {
	for _, name := range []string{types.VaultProjectsDir, types.VaultBundlesDir} {
		info, err := os.Stat(filepath.Join(repoDir, name))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
