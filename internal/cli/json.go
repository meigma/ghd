package cli

import (
	"encoding/json"
	"time"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/state"
)

type packageListJSON struct {
	Packages []packageListItemJSON `json:"packages"`
}

type packageListItemJSON struct {
	Repository string   `json:"repository"`
	Package    string   `json:"package"`
	Target     string   `json:"target"`
	Binaries   []string `json:"binaries"`
}

type packageInfoJSON struct {
	Package packageInfoItemJSON `json:"package"`
}

type packageInfoItemJSON struct {
	Repository     string                 `json:"repository"`
	Package        string                 `json:"package"`
	Target         string                 `json:"target"`
	SignerWorkflow string                 `json:"signer_workflow"`
	TagPattern     string                 `json:"tag_pattern"`
	Binaries       []string               `json:"binaries"`
	Assets         []packageInfoAssetJSON `json:"assets"`
}

type packageInfoAssetJSON struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Pattern string `json:"pattern"`
}

type installedListJSON struct {
	Installed []installedPackageJSON `json:"installed"`
}

type installedPackageJSON struct {
	Repository       string                `json:"repository"`
	Package          string                `json:"package"`
	Target           string                `json:"target"`
	Version          string                `json:"version"`
	Tag              string                `json:"tag"`
	Asset            string                `json:"asset"`
	AssetDigest      string                `json:"asset_digest"`
	StorePath        string                `json:"store_path"`
	ArtifactPath     string                `json:"artifact_path"`
	ExtractedPath    string                `json:"extracted_path"`
	VerificationPath string                `json:"verification_path"`
	InstalledAt      time.Time             `json:"installed_at"`
	Binaries         []installedBinaryJSON `json:"binaries"`
}

type installedBinaryJSON struct {
	Name       string `json:"name"`
	LinkPath   string `json:"link_path"`
	TargetPath string `json:"target_path"`
}

type checkResultsJSON struct {
	Checks []checkResultJSON `json:"checks"`
}

type checkResultJSON struct {
	Repository    string `json:"repository"`
	Package       string `json:"package"`
	Target        string `json:"target"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	LatestVersion string `json:"latest_version"`
	Reason        string `json:"reason"`
}

type verifyResultsJSON struct {
	Verifications []verifyResultJSON `json:"verifications"`
}

type verifyResultJSON struct {
	Repository string `json:"repository"`
	Package    string `json:"package"`
	Target     string `json:"target"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
}

type updateResultsJSON struct {
	Updates []updateResultJSON `json:"updates"`
}

type updateResultJSON struct {
	Repository      string `json:"repository"`
	Package         string `json:"package"`
	Target          string `json:"target"`
	PreviousVersion string `json:"previous_version"`
	CurrentVersion  string `json:"current_version"`
	Status          string `json:"status"`
	Reason          string `json:"reason"`
}

type doctorResultsJSON struct {
	Checks []doctorResultJSON `json:"checks"`
}

type doctorResultJSON struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type repositoryListJSON struct {
	Repositories []repositoryJSON `json:"repositories"`
}

type repositoryJSON struct {
	Repository  string                  `json:"repository"`
	Packages    []repositoryPackageJSON `json:"packages"`
	RefreshedAt time.Time               `json:"refreshed_at"`
}

type repositoryPackageJSON struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Binaries    []string `json:"binaries"`
}

func writeJSON(options Options, value any) error {
	return json.NewEncoder(options.Out).Encode(value)
}

func writePackageListJSON(options Options, results []app.PackageListResult) error {
	packages := make([]packageListItemJSON, 0, len(results))
	for _, result := range results {
		repository := result.Repository.String()
		packages = append(packages, packageListItemJSON{
			Repository: repository,
			Package:    result.PackageName.String(),
			Target:     packageTarget(repository, result.PackageName.String()),
			Binaries:   copyStrings(result.Binaries),
		})
	}
	return writeJSON(options, packageListJSON{Packages: packages})
}

func writePackageInfoJSON(options Options, result app.PackageInfoResult) error {
	assets := make([]packageInfoAssetJSON, 0, len(result.Assets))
	for _, asset := range result.Assets {
		assets = append(assets, packageInfoAssetJSON{
			OS:      asset.OS,
			Arch:    asset.Arch,
			Pattern: asset.Pattern,
		})
	}
	repository := result.Repository.String()
	return writeJSON(options, packageInfoJSON{
		Package: packageInfoItemJSON{
			Repository:     repository,
			Package:        result.PackageName.String(),
			Target:         packageTarget(repository, result.PackageName.String()),
			SignerWorkflow: string(result.SignerWorkflow),
			TagPattern:     result.TagPattern,
			Binaries:       copyStrings(result.Binaries),
			Assets:         assets,
		},
	})
}

func writeInstalledListJSON(options Options, records []state.Record) error {
	installed := make([]installedPackageJSON, 0, len(records))
	for _, record := range records {
		binaries := make([]installedBinaryJSON, 0, len(record.Binaries))
		for _, binary := range record.Binaries {
			binaries = append(binaries, installedBinaryJSON{
				Name:       binary.Name,
				LinkPath:   binary.LinkPath,
				TargetPath: binary.TargetPath,
			})
		}
		installed = append(installed, installedPackageJSON{
			Repository:       record.Repository,
			Package:          record.Package,
			Target:           packageTarget(record.Repository, record.Package),
			Version:          record.Version,
			Tag:              record.Tag,
			Asset:            record.Asset,
			AssetDigest:      record.AssetDigest,
			StorePath:        record.StorePath,
			ArtifactPath:     record.ArtifactPath,
			ExtractedPath:    record.ExtractedPath,
			VerificationPath: record.VerificationPath,
			InstalledAt:      record.InstalledAt,
			Binaries:         binaries,
		})
	}
	return writeJSON(options, installedListJSON{Installed: installed})
}

func writeCheckResultsJSON(options Options, results []app.CheckResult) error {
	checks := make([]checkResultJSON, 0, len(results))
	for _, result := range results {
		checks = append(checks, checkResultJSON{
			Repository:    result.Repository,
			Package:       result.Package,
			Target:        packageTarget(result.Repository, result.Package),
			Version:       result.Version,
			Status:        string(result.Status),
			LatestVersion: result.LatestVersion,
			Reason:        result.Reason,
		})
	}
	return writeJSON(options, checkResultsJSON{Checks: checks})
}

func writeVerifyResultsJSON(options Options, results []app.VerifyInstalledResult) error {
	verifications := make([]verifyResultJSON, 0, len(results))
	for _, result := range results {
		verifications = append(verifications, verifyResultJSON{
			Repository: result.Repository,
			Package:    result.Package,
			Target:     packageTarget(result.Repository, result.Package),
			Version:    result.Version,
			Status:     string(result.Status),
			Reason:     result.Reason,
		})
	}
	return writeJSON(options, verifyResultsJSON{Verifications: verifications})
}

func writeUpdateResultsJSON(options Options, results []app.UpdateInstalledResult) error {
	updates := make([]updateResultJSON, 0, len(results))
	for _, result := range results {
		updates = append(updates, updateResultJSON{
			Repository:      result.Repository,
			Package:         result.Package,
			Target:          packageTarget(result.Repository, result.Package),
			PreviousVersion: result.PreviousVersion,
			CurrentVersion:  result.CurrentVersion,
			Status:          string(result.Status),
			Reason:          result.Reason,
		})
	}
	return writeJSON(options, updateResultsJSON{Updates: updates})
}

func writeDoctorResultsJSON(options Options, results []app.DoctorResult) error {
	checks := make([]doctorResultJSON, 0, len(results))
	for _, result := range results {
		checks = append(checks, doctorResultJSON{
			ID:      result.ID,
			Status:  string(result.Status),
			Message: result.Message,
		})
	}
	return writeJSON(options, doctorResultsJSON{Checks: checks})
}

func writeRepositoryListJSON(options Options, repositories []catalog.RepositoryRecord) error {
	rows := make([]repositoryJSON, 0, len(repositories))
	for _, record := range repositories {
		packages := make([]repositoryPackageJSON, 0, len(record.Packages))
		for _, pkg := range record.Packages {
			packages = append(packages, repositoryPackageJSON{
				Name:        pkg.Name,
				Description: pkg.Description,
				Binaries:    copyStrings(pkg.Binaries),
			})
		}
		rows = append(rows, repositoryJSON{
			Repository:  record.Repository.String(),
			Packages:    packages,
			RefreshedAt: record.RefreshedAt,
		})
	}
	return writeJSON(options, repositoryListJSON{Repositories: rows})
}

func packageTarget(repository string, packageName string) string {
	if repository == "" {
		return packageName
	}
	if packageName == "" {
		return repository
	}
	return repository + "/" + packageName
}

func copyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}
