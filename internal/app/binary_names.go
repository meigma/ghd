package app

import (
	"path"
	"strings"

	"github.com/meigma/ghd/internal/manifest"
)

func manifestBinaryNames(binaries []manifest.Binary) []string {
	names := make([]string, 0, len(binaries))
	for _, binary := range binaries {
		normalized := strings.ReplaceAll(strings.TrimSpace(binary.Path), "\\", "/")
		names = append(names, path.Base(normalized))
	}
	return names
}
