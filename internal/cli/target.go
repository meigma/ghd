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
	qualified   bool
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
		qualified:   true,
	}, nil
}

func parseInstallTarget(value string) (packageVersionTarget, error) {
	value = strings.TrimSpace(value)
	targetPart, version, found := strings.Cut(value, "@")
	if !found || strings.TrimSpace(version) == "" {
		return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
	}
	if strings.Contains(version, "/") {
		return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
	}
	parts := strings.Split(targetPart, "/")
	switch len(parts) {
	case 1:
		if strings.TrimSpace(parts[0]) == "" {
			return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
		}
		return packageVersionTarget{packageName: parts[0], version: version}, nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
		}
		return packageVersionTarget{
			repository: verification.Repository{
				Owner: parts[0],
				Name:  parts[1],
			},
			packageName: parts[2],
			version:     version,
			qualified:   true,
		}, nil
	default:
		return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
	}
}

func parseRepositoryTarget(value string) (verification.Repository, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return verification.Repository{}, fmt.Errorf("repository must be owner/repo")
	}
	return verification.Repository{Owner: parts[0], Name: parts[1]}, nil
}

func parseUninstallTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("uninstall target must be name or owner/repo/package")
	}
	parts := strings.Split(value, "/")
	switch len(parts) {
	case 1:
		if strings.TrimSpace(parts[0]) == "" {
			return "", fmt.Errorf("uninstall target must be name or owner/repo/package")
		}
		return parts[0], nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return "", fmt.Errorf("uninstall target must be name or owner/repo/package")
		}
		return value, nil
	default:
		return "", fmt.Errorf("uninstall target must be name or owner/repo/package")
	}
}
