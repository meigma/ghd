package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/klauspost/compress/snappy"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/meigma/ghd/internal/app"
	"github.com/meigma/ghd/internal/verification"
)

const (
	// DefaultBaseURL is the public GitHub REST API endpoint.
	DefaultBaseURL = "https://api.github.com"
	// DefaultAPIVersion is the GitHub REST API version used by ghd.
	DefaultAPIVersion = "2026-03-10"
	defaultPerPage    = 100
	defaultMaxResults = 1000
)

// Client implements verification release and attestation lookup ports with GitHub REST.
type Client struct {
	httpClient      *http.Client
	baseURL         *url.URL
	token           string
	apiVersion      string
	userAgent       string
	maxAttestations int
}

type clientOptions struct {
	httpClient      *http.Client
	baseURL         string
	token           string
	apiVersion      string
	userAgent       string
	maxAttestations int
}

// Option configures a GitHub REST client.
type Option func(*clientOptions)

// WithHTTPClient sets the HTTP client used for GitHub and bundle URL requests.
func WithHTTPClient(client *http.Client) Option {
	return func(opts *clientOptions) {
		opts.httpClient = client
	}
}

// WithBaseURL sets the GitHub REST API base URL.
func WithBaseURL(baseURL string) Option {
	return func(opts *clientOptions) {
		opts.baseURL = baseURL
	}
}

// WithToken sets the optional GitHub bearer token.
func WithToken(token string) Option {
	return func(opts *clientOptions) {
		opts.token = token
	}
}

// WithAPIVersion sets the X-GitHub-Api-Version header.
func WithAPIVersion(apiVersion string) Option {
	return func(opts *clientOptions) {
		opts.apiVersion = apiVersion
	}
}

// WithUserAgent sets the optional User-Agent header.
func WithUserAgent(userAgent string) Option {
	return func(opts *clientOptions) {
		opts.userAgent = userAgent
	}
}

// WithMaxAttestations bounds attestation pagination.
func WithMaxAttestations(maxAttestations int) Option {
	return func(opts *clientOptions) {
		opts.maxAttestations = maxAttestations
	}
}

// NewClient creates a GitHub REST adapter.
func NewClient(options ...Option) (*Client, error) {
	opts := clientOptions{}
	for _, option := range options {
		option(&opts)
	}

	httpClient := opts.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	base := opts.baseURL
	if base == "" {
		base = DefaultBaseURL
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub base URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("GitHub base URL must be absolute")
	}

	apiVersion := opts.apiVersion
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}

	maxAttestations := opts.maxAttestations
	if maxAttestations == 0 {
		maxAttestations = defaultMaxResults
	}
	if maxAttestations < 0 {
		return nil, fmt.Errorf("max attestations must be non-negative")
	}

	return &Client{
		httpClient:      httpClient,
		baseURL:         baseURL,
		token:           opts.token,
		apiVersion:      apiVersion,
		userAgent:       opts.userAgent,
		maxAttestations: maxAttestations,
	}, nil
}

// ResolveReleaseTag resolves a GitHub release tag to the tag ref object digest.
func (c *Client) ResolveReleaseTag(ctx context.Context, repository verification.Repository, tag verification.ReleaseTag) (verification.Digest, error) {
	req, err := c.newGitHubRequest(ctx, http.MethodGet, releaseTagPath(repository, tag), nil)
	if err != nil {
		return verification.Digest{}, err
	}

	var ref gitRefResponse
	if err := c.doJSON(req, &ref); err != nil {
		return verification.Digest{}, err
	}
	if ref.Object.SHA == "" {
		return verification.Digest{}, fmt.Errorf("GitHub ref response did not include object SHA")
	}
	return verification.NewDigest("sha1", ref.Object.SHA)
}

// FetchManifest returns the root ghd.toml for repository.
func (c *Client) FetchManifest(ctx context.Context, repository verification.Repository) ([]byte, error) {
	req, err := c.newGitHubRequest(ctx, http.MethodGet, rawPath(fmt.Sprintf("repos/%s/%s/contents/ghd.toml", repository.Owner, repository.Name)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw")

	resp, err := c.doRawResponse(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read ghd.toml response: %w", err)
	}
	return decodeManifestBody(body)
}

// ResolveReleaseAsset returns the exact matching asset for tag.
func (c *Client) ResolveReleaseAsset(ctx context.Context, repository verification.Repository, tag verification.ReleaseTag, assetName string) (app.ReleaseAsset, error) {
	req, err := c.newGitHubRequest(ctx, http.MethodGet, releaseByTagPath(repository, tag), nil)
	if err != nil {
		return app.ReleaseAsset{}, err
	}

	var release releaseResponse
	if err := c.doJSON(req, &release); err != nil {
		return app.ReleaseAsset{}, err
	}

	matches := make([]releaseAssetResponse, 0, 1)
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			matches = append(matches, asset)
		}
	}
	switch len(matches) {
	case 0:
		return app.ReleaseAsset{}, fmt.Errorf("release %s has no asset named %q", tag, assetName)
	case 1:
		if matches[0].BrowserDownloadURL == "" {
			return app.ReleaseAsset{}, fmt.Errorf("release asset %q has no browser_download_url", assetName)
		}
		return app.ReleaseAsset{Name: matches[0].Name, DownloadURL: matches[0].BrowserDownloadURL}, nil
	default:
		return app.ReleaseAsset{}, fmt.Errorf("release %s has multiple assets named %q", tag, assetName)
	}
}

// ListRepositoryReleases returns the repository's GitHub releases.
func (c *Client) ListRepositoryReleases(ctx context.Context, repository verification.Repository) ([]app.RepositoryRelease, error) {
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(defaultPerPage))

	req, err := c.newGitHubRequest(ctx, http.MethodGet, releasesPath(repository), query)
	if err != nil {
		return nil, err
	}

	releases := make([]app.RepositoryRelease, 0, defaultPerPage)
	for req != nil {
		var response []releaseResponse
		resp, err := c.doJSONResponse(req, &response)
		if err != nil {
			return nil, err
		}
		for _, release := range response {
			assetNames := make([]string, 0, len(release.Assets))
			for _, asset := range release.Assets {
				assetNames = append(assetNames, asset.Name)
			}
			releases = append(releases, app.RepositoryRelease{
				TagName:    release.TagName,
				Draft:      release.Draft,
				Prerelease: release.Prerelease,
				AssetNames: assetNames,
			})
		}
		next, err := c.nextRequest(ctx, resp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		req = next
	}
	return releases, nil
}

// DownloadReleaseAsset downloads asset into outputDir without setting executable bits.
func (c *Client) DownloadReleaseAsset(ctx context.Context, asset app.ReleaseAsset, outputDir string) (string, error) {
	if asset.Name == "" {
		return "", fmt.Errorf("release asset name must be set")
	}
	if asset.DownloadURL == "" {
		return "", fmt.Errorf("release asset download URL must be set")
	}
	if outputDir == "" {
		return "", fmt.Errorf("output directory must be set")
	}
	if err := validateAssetFilename(asset.Name); err != nil {
		return "", err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download release asset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", httpStatusError(resp)
	}

	temp, err := os.CreateTemp(outputDir, "."+asset.Name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary artifact: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(temp, resp.Body); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temporary artifact: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary artifact: %w", err)
	}

	finalPath := filepath.Join(outputDir, asset.Name)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("commit artifact: %w", err)
	}
	removeTemp = false
	return finalPath, nil
}

// FetchReleaseAttestations returns GitHub release attestations for a tag ref digest.
func (c *Client) FetchReleaseAttestations(ctx context.Context, repository verification.Repository, tagDigest verification.Digest) ([]verification.Attestation, error) {
	return c.fetchAttestations(ctx, repository, tagDigest, "release")
}

// FetchProvenanceAttestations returns GitHub provenance attestations for an artifact digest.
func (c *Client) FetchProvenanceAttestations(ctx context.Context, repository verification.Repository, artifactDigest verification.Digest) ([]verification.Attestation, error) {
	return c.fetchAttestations(ctx, repository, artifactDigest, "provenance")
}

func (c *Client) fetchAttestations(ctx context.Context, repository verification.Repository, digest verification.Digest, predicateType string) ([]verification.Attestation, error) {
	path := rawPath(fmt.Sprintf("repos/%s/%s/attestations/%s", repository.Owner, repository.Name, digest.String()))
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(defaultPerPage))
	query.Set("predicate_type", predicateType)

	req, err := c.newGitHubRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	var out []verification.Attestation
	for req != nil {
		var response attestationsResponse
		resp, err := c.doJSONResponse(req, &response)
		if err != nil {
			return nil, err
		}

		for i, attestation := range response.Attestations {
			if attestation.BundleURL == "" {
				resp.Body.Close()
				return nil, fmt.Errorf("attestation response entry %d has no bundle_url", i)
			}
			bundle, err := c.fetchBundle(ctx, attestation.BundleURL)
			if err != nil {
				resp.Body.Close()
				return nil, err
			}
			out = append(out, verification.Attestation{
				ID:     attestation.id(),
				Bundle: bundle,
			})
			if c.maxAttestations > 0 && len(out) > c.maxAttestations {
				resp.Body.Close()
				return nil, fmt.Errorf("GitHub returned more than %d attestations for %s", c.maxAttestations, digest)
			}
		}

		next, err := c.nextRequest(ctx, resp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		req = next
	}

	return out, nil
}

func (c *Client) fetchBundle(ctx context.Context, bundleURL string) (*sigbundle.Bundle, error) {
	parsed, err := url.Parse(bundleURL)
	if err != nil {
		return nil, fmt.Errorf("parse bundle_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("bundle_url must be absolute")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bundleURL, nil)
	if err != nil {
		return nil, err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch attestation bundle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, httpStatusError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read attestation bundle response: %w", err)
	}

	decompressed, err := snappy.Decode(nil, body)
	if err != nil {
		return nil, fmt.Errorf("decompress attestation bundle: %w", err)
	}

	var pbBundle protobundle.Bundle
	if err := protojson.Unmarshal(decompressed, &pbBundle); err != nil {
		return nil, fmt.Errorf("parse attestation bundle: %w", err)
	}

	bundle, err := sigbundle.NewBundle(&pbBundle)
	if err != nil {
		return nil, fmt.Errorf("validate attestation bundle: %w", err)
	}
	return bundle, nil
}

func (c *Client) newGitHubRequest(ctx context.Context, method string, path rawPath, query url.Values) (*http.Request, error) {
	u := c.resolvePath(path)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.setGitHubHeaders(req)
	return req, nil
}

func (c *Client) newGitHubRequestFromURL(ctx context.Context, next string) (*http.Request, error) {
	u, err := url.Parse(next)
	if err != nil {
		return nil, fmt.Errorf("parse next page URL: %w", err)
	}
	if u.Scheme != c.baseURL.Scheme || u.Host != c.baseURL.Host {
		return nil, fmt.Errorf("next page URL host %q does not match GitHub API host %q", u.Host, c.baseURL.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.setGitHubHeaders(req)
	return req, nil
}

func (c *Client) setGitHubHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", c.apiVersion)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
}

type rawPath string

func (c *Client) resolvePath(path rawPath) url.URL {
	u := *c.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	cleanPath := strings.TrimLeft(string(path), "/")
	u.Path = basePath + "/" + cleanPath
	escapedPath := basePath + "/" + escapePathSegments(cleanPath)
	if escapedPath != u.EscapedPath() {
		u.RawPath = escapedPath
	}
	return u
}

func (c *Client) doJSON(req *http.Request, target any) error {
	resp, err := c.doJSONResponse(req, target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) doRawResponse(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, httpStatusError(resp)
	}
	return resp, nil
}

func (c *Client) doJSONResponse(req *http.Request, target any) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, httpStatusError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("decode GitHub response: %w", err)
	}
	return resp, nil
}

func (c *Client) nextRequest(ctx context.Context, resp *http.Response) (*http.Request, error) {
	next := parseNextLink(resp.Header.Get("Link"))
	if next == "" {
		return nil, nil
	}
	return c.newGitHubRequestFromURL(ctx, next)
}

func httpStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("%s %s returned HTTP %d", resp.Request.Method, resp.Request.URL, resp.StatusCode)
	}
	return fmt.Errorf("%s %s returned HTTP %d: %s", resp.Request.Method, resp.Request.URL, resp.StatusCode, message)
}

func parseNextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		target := strings.TrimSpace(segments[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, parameter := range segments[1:] {
			if strings.TrimSpace(parameter) == `rel="next"` {
				return strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
			}
		}
	}
	return ""
}

func releaseTagPath(repository verification.Repository, tag verification.ReleaseTag) rawPath {
	return rawPath(fmt.Sprintf("repos/%s/%s/git/ref/tags/%s", repository.Owner, repository.Name, tag))
}

func releaseByTagPath(repository verification.Repository, tag verification.ReleaseTag) rawPath {
	return rawPath(fmt.Sprintf("repos/%s/%s/releases/tags/%s", repository.Owner, repository.Name, tag))
}

func releasesPath(repository verification.Repository) rawPath {
	return rawPath(fmt.Sprintf("repos/%s/%s/releases", repository.Owner, repository.Name))
}

func validateAssetFilename(name string) error {
	if name == "." || name == ".." || strings.TrimSpace(name) == "" {
		return fmt.Errorf("release asset name %q is not a safe filename", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || filepath.Base(name) != name {
		return fmt.Errorf("release asset name %q must not contain path separators", name)
	}
	return nil
}

func escapePathSegments(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

type gitRefResponse struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type contentResponse struct {
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func decodeManifestBody(body []byte) ([]byte, error) {
	var content contentResponse
	if err := json.Unmarshal(body, &content); err != nil {
		return body, nil
	}
	if !strings.EqualFold(content.Encoding, "base64") || content.Content == "" {
		return body, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode ghd.toml content: %w", err)
	}
	return decoded, nil
}

type releaseResponse struct {
	TagName    string                 `json:"tag_name"`
	Draft      bool                   `json:"draft"`
	Prerelease bool                   `json:"prerelease"`
	Assets     []releaseAssetResponse `json:"assets"`
}

type releaseAssetResponse struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type attestationsResponse struct {
	Attestations []attestationResponse `json:"attestations"`
}

type attestationResponse struct {
	ID        json.RawMessage `json:"id"`
	BundleURL string          `json:"bundle_url"`
}

func (a attestationResponse) id() string {
	if len(a.ID) != 0 {
		return strings.Trim(string(a.ID), `"`)
	}
	return a.BundleURL
}
