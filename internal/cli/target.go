package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

const (
	installTargetError      = "install target must be package, package@version, owner/repo/package, or owner/repo/package@version"
	nameTargetPartCount     = 1
	repositoryTargetParts   = 2
	packageTargetPartCount  = 3
	qualifiedTargetPartZero = 0
	qualifiedTargetPartOne  = 1
	qualifiedTargetPartTwo  = 2
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
	if len(parts) != packageTargetPartCount ||
		parts[qualifiedTargetPartZero] == "" ||
		parts[qualifiedTargetPartOne] == "" ||
		parts[qualifiedTargetPartTwo] == "" {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	if strings.Contains(version, "/") {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	repository, err := verification.NewRepository(parts[qualifiedTargetPartZero], parts[qualifiedTargetPartOne])
	if err != nil {
		return packageVersionTarget{}, fmt.Errorf("%s target must be owner/repo/package@version", command)
	}
	packageName, err := manifest.NewPackageName(parts[qualifiedTargetPartTwo])
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
	var packageVersion manifest.PackageVersion
	if found {
		if strings.TrimSpace(version) == "" {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
		if strings.Contains(version, "/") {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
		var err error
		packageVersion, err = manifest.NewPackageVersion(version)
		if err != nil {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
	}
	parts := strings.Split(targetPart, "/")
	switch len(parts) {
	case nameTargetPartCount:
		packageName, err := manifest.NewPackageName(parts[qualifiedTargetPartZero])
		if err != nil {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
		return packageVersionTarget{packageName: packageName, version: packageVersion}, nil
	case packageTargetPartCount:
		if parts[qualifiedTargetPartZero] == "" ||
			parts[qualifiedTargetPartOne] == "" ||
			parts[qualifiedTargetPartTwo] == "" {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
		repository, err := verification.NewRepository(parts[qualifiedTargetPartZero], parts[qualifiedTargetPartOne])
		if err != nil {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
		packageName, err := manifest.NewPackageName(parts[qualifiedTargetPartTwo])
		if err != nil {
			return packageVersionTarget{}, errors.New(installTargetError)
		}
		return packageVersionTarget{
			repository:  repository,
			packageName: packageName,
			version:     packageVersion,
			qualified:   true,
		}, nil
	default:
		return packageVersionTarget{}, errors.New(installTargetError)
	}
}

func parseRepositoryTarget(value string) (verification.Repository, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return verification.Repository{}, errors.New("repository must be owner/repo")
	}
	repository, err := verification.ParseRepository(value)
	if err != nil {
		return verification.Repository{}, errors.New("repository must be owner/repo")
	}
	return repository, nil
}

func parseUninstallTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", errors.New("uninstall target must be name or owner/repo/package")
	}
	return target, nil
}

func parseCheckTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", errors.New("check target must be name or owner/repo/package")
	}
	return target, nil
}

func parseUpdateTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", errors.New("update target must be name or owner/repo/package")
	}
	return target, nil
}

func parseVerifyTarget(value string) (string, error) {
	target, err := parseNamedOrQualifiedTarget(value)
	if err != nil {
		return "", errors.New("verify target must be name or owner/repo/package")
	}
	return target, nil
}

func parseListTarget(value string) (verification.Repository, error) {
	repository, err := parseRepositoryTarget(value)
	if err != nil {
		return verification.Repository{}, errors.New("list target must be owner/repo")
	}
	return repository, nil
}

func parseInfoTarget(value string) (packageInfoTarget, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
	}
	if strings.Contains(value, "@") {
		return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
	}
	parts := strings.Split(value, "/")
	switch len(parts) {
	case nameTargetPartCount:
		if strings.TrimSpace(parts[qualifiedTargetPartZero]) == "" {
			return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
		}
		return packageInfoTarget{unqualifiedName: parts[qualifiedTargetPartZero]}, nil
	case repositoryTargetParts:
		repository, err := verification.NewRepository(parts[qualifiedTargetPartZero], parts[qualifiedTargetPartOne])
		if err != nil {
			return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
		}
		return packageInfoTarget{
			repository: repository,
		}, nil
	case packageTargetPartCount:
		if parts[qualifiedTargetPartZero] == "" ||
			parts[qualifiedTargetPartOne] == "" ||
			parts[qualifiedTargetPartTwo] == "" {
			return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
		}
		repository, err := verification.NewRepository(parts[qualifiedTargetPartZero], parts[qualifiedTargetPartOne])
		if err != nil {
			return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
		}
		packageName, err := manifest.NewPackageName(parts[qualifiedTargetPartTwo])
		if err != nil {
			return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
		}
		return packageInfoTarget{
			repository:  repository,
			packageName: packageName,
		}, nil
	default:
		return packageInfoTarget{}, errors.New("info target must be name, owner/repo, or owner/repo/package")
	}
}

func parseNamedOrQualifiedTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("target must be set")
	}
	parts := strings.Split(value, "/")
	switch len(parts) {
	case nameTargetPartCount:
		if strings.TrimSpace(parts[qualifiedTargetPartZero]) == "" {
			return "", errors.New("target must be set")
		}
		return parts[qualifiedTargetPartZero], nil
	case packageTargetPartCount:
		if parts[qualifiedTargetPartZero] == "" ||
			parts[qualifiedTargetPartOne] == "" ||
			parts[qualifiedTargetPartTwo] == "" {
			return "", errors.New("target must be set")
		}
		return value, nil
	default:
		return "", errors.New("target must be name or owner/repo/package")
	}
}
