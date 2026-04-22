package cli

import (
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/verification"
)

type packageVersionTarget struct {
	repository  verification.Repository
	packageName string
	version     string
}

func parsePackageVersionTarget(command string, value string) (packageVersionTarget, error) {
	value = strings.TrimSpace(value)
	targetPart, version, found := strings.Cut(value, "@")
	if !found || strings.TrimSpace(version) == "" {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	parts := strings.Split(targetPart, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	if strings.Contains(version, "/") {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	return packageVersionTarget{
		repository: verification.Repository{
			Owner: parts[0],
			Name:  parts[1],
		},
		packageName: parts[2],
		version:     version,
	}, nil
}
