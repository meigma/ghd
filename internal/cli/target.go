package cli

import (
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

type packageVersionTarget struct {
	repository  verification.Repository
	packageName manifest.PackageName
	version     manifest.PackageVersion
	qualified   bool
}

type packageInfoTarget struct {
	repository      verification.Repository
	packageName     manifest.PackageName
	unqualifiedName string
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
	repository, err := verification.NewRepository(parts[0], parts[1])
	if err != nil {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	packageName, err := manifest.NewPackageName(parts[2])
	if err != nil {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	packageVersion, err := manifest.NewPackageVersion(version)
	if err != nil {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	return packageVersionTarget{
		repository:  repository,
		packageName: packageName,
		version:     packageVersion,
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
	packageVersion, err := manifest.NewPackageVersion(version)
	if err != nil {
		return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
	}
	parts := strings.Split(targetPart, "/")
	switch len(parts) {
	case 1:
		packageName, err := manifest.NewPackageName(parts[0])
		if err != nil {
			return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
		}
		return packageVersionTarget{packageName: packageName, version: packageVersion}, nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
		}
		repository, err := verification.NewRepository(parts[0], parts[1])
		if err != nil {
			return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
		}
		packageName, err := manifest.NewPackageName(parts[2])
		if err != nil {
			return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
		}
		return packageVersionTarget{
			repository:  repository,
			packageName: packageName,
			version:     packageVersion,
			qualified:   true,
		}, nil
	default:
		return packageVersionTarget{}, fmt.Errorf("install target must be package@version or owner/repo/package@version")
	}
}

func parseRepositoryTarget(value string) (verification.Repository, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return verification.Repository{}, fmt.Errorf("repository must be owner/repo")
	}
	repository, err := verification.ParseRepository(value)
	if err != nil {
		return verification.Repository{}, fmt.Errorf("repository must be owner/repo")
	}
	return repository, nil
}

func parseUninstallTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", fmt.Errorf("uninstall target must be name or owner/repo/package")
	}
	return target, nil
}

func parseCheckTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", fmt.Errorf("check target must be name or owner/repo/package")
	}
	return target, nil
}

func parseUpdateTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", fmt.Errorf("update target must be name or owner/repo/package")
	}
	return target, nil
}

func parseVerifyTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", fmt.Errorf("verify target must be name or owner/repo/package")
	}
	return target, nil
}

func parseListTarget(value string) (verification.Repository, error) {
	repository, err := parseRepositoryTarget(value)
	if err != nil {
		return verification.Repository{}, fmt.Errorf("list target must be owner/repo")
	}
	return repository, nil
}

func parseInfoTarget(value string) (packageInfoTarget, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
	}
	if strings.Contains(value, "@") {
		return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
	}
	parts := strings.Split(value, "/")
	switch len(parts) {
	case 1:
		if strings.TrimSpace(parts[0]) == "" {
			return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
		}
		return packageInfoTarget{unqualifiedName: parts[0]}, nil
	case 2:
		repository, err := verification.NewRepository(parts[0], parts[1])
		if err != nil {
			return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
		}
		return packageInfoTarget{
			repository: repository,
		}, nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
		}
		repository, err := verification.NewRepository(parts[0], parts[1])
		if err != nil {
			return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
		}
		packageName, err := manifest.NewPackageName(parts[2])
		if err != nil {
			return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
		}
		return packageInfoTarget{
			repository:  repository,
			packageName: packageName,
		}, nil
	default:
		return packageInfoTarget{}, fmt.Errorf("info target must be name, owner/repo, or owner/repo/package")
	}
}

func parseNamedOrQualifiedTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("target must be set")
	}
	parts := strings.Split(value, "/")
	switch len(parts) {
	case 1:
		if strings.TrimSpace(parts[0]) == "" {
			return "", fmt.Errorf("target must be set")
		}
		return parts[0], nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return "", fmt.Errorf("target must be set")
		}
		return value, nil
	default:
		return "", fmt.Errorf("target must be name or owner/repo/package")
	}
}
