// Package updater replaces the running psl executable with the newest release
// published on GitHub by release.sh.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultRepo is the GitHub repository releases are published to.
	DefaultRepo = "lhypds/psl"
	// DefaultAPIBase is the root of the GitHub REST API.
	DefaultAPIBase = "https://api.github.com"
	// ChecksumsAsset is the file release.sh publishes alongside the archives.
	ChecksumsAsset = "SHA256SUMS"

	// maxDownload caps an asset, so a wrong URL cannot fill the disk.
	maxDownload = 256 << 20
	timeout     = 5 * time.Minute
)

// Options configures an update.
type Options struct {
	Repo    string // "owner/name"; defaults to DefaultRepo
	Current string // version running now, without a leading "v"
	GOOS    string // defaults to runtime.GOOS
	GOARCH  string // defaults to runtime.GOARCH
	APIBase string // defaults to DefaultAPIBase
	ExePath string // executable to replace; defaults to the running one
	HTTP    *http.Client
	Log     io.Writer // progress messages; nil discards them
}

// Result describes what an update did.
type Result struct {
	Previous string // version that was running
	Latest   string // newest released version
	Updated  bool   // false when the newest release was already installed
	Path     string // executable that was replaced
	URL      string // release page
}

type release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Update downloads the newest release for this platform, verifies it against
// the release's SHA256SUMS, and swaps it in for the running executable.
func Update(ctx context.Context, opts Options) (*Result, error) {
	if err := opts.setDefaults(); err != nil {
		return nil, err
	}

	opts.logf("checking %s for a newer release…", opts.Repo)
	rel, err := opts.fetchLatest(ctx)
	if err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if latest == "" {
		return nil, fmt.Errorf("the latest release of %s has no tag name", opts.Repo)
	}
	result := &Result{Previous: opts.Current, Latest: latest, Path: opts.ExePath, URL: rel.HTMLURL}
	if latest == opts.Current {
		return result, nil
	}

	assetName := archiveName(latest, opts.GOOS, opts.GOARCH)
	asset, err := rel.find(assetName)
	if err != nil {
		return nil, err
	}
	sums, err := rel.find(ChecksumsAsset)
	if err != nil {
		return nil, fmt.Errorf("%w; psl will not install a release it cannot verify", err)
	}

	opts.logf("downloading %s…", ChecksumsAsset)
	sumsText, err := opts.get(ctx, sums.URL)
	if err != nil {
		return nil, err
	}
	want, err := expectedSum(string(sumsText), assetName)
	if err != nil {
		return nil, err
	}

	opts.logf("downloading %s…", assetName)
	archive, err := opts.download(ctx, asset.URL, want)
	if err != nil {
		return nil, err
	}
	defer os.Remove(archive)

	opts.logf("installing into %s…", opts.ExePath)
	if err := install(archive, assetName, binaryName(opts.GOOS), opts.ExePath); err != nil {
		return nil, err
	}
	result.Updated = true
	return result, nil
}

func (o *Options) setDefaults() error {
	if o.Repo == "" {
		o.Repo = DefaultRepo
	}
	if o.APIBase == "" {
		o.APIBase = DefaultAPIBase
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: timeout}
	}
	if o.ExePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate the running executable: %w", err)
		}
		o.ExePath = exe
	}
	// Follow symlinks so that an update replaces the real binary rather than
	// turning the link into a file.
	if resolved, err := filepath.EvalSymlinks(o.ExePath); err == nil {
		o.ExePath = resolved
	}
	return nil
}

func (o *Options) logf(format string, args ...any) {
	if o.Log == nil {
		return
	}
	fmt.Fprintf(o.Log, "psl: "+format+"\n", args...)
}

func (o *Options) fetchLatest(ctx context.Context) (*release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(o.APIBase, "/"), o.Repo)
	body, err := o.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("decode the release listing from %s: %w", url, err)
	}
	return &rel, nil
}

// get fetches a small document: the release listing or the checksums file.
func (o *Options) get(ctx context.Context, url string) ([]byte, error) {
	resp, err := o.do(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (o *Options) do(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "psl/"+o.Current)
	// A token is not required, but it lifts GitHub's anonymous rate limit.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%s returned 404: %s has no published release for this platform yet", url, o.Repo)
		}
		return nil, fmt.Errorf("%s returned %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

// download streams an asset to a temporary file, checking its digest as it goes.
func (o *Options) download(ctx context.Context, url, wantSum string) (string, error) {
	resp, err := o.do(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp("", "psl-update-*")
	if err != nil {
		return "", fmt.Errorf("create a temporary file: %w", err)
	}
	name := tmp.Name()

	digest := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, digest), io.LimitReader(resp.Body, maxDownload))
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != wantSum {
		os.Remove(name)
		return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantSum)
	}
	return name, nil
}

func (r *release) find(name string) (asset, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	var zero asset
	available := make([]string, 0, len(r.Assets))
	for _, a := range r.Assets {
		available = append(available, a.Name)
	}
	if len(available) == 0 {
		return zero, fmt.Errorf("release %s has no assets", r.TagName)
	}
	return zero, fmt.Errorf("release %s has no asset named %s (it has: %s)",
		r.TagName, name, strings.Join(available, ", "))
}

// expectedSum reads one entry out of a `shasum -a 256` listing.
func expectedSum(listing, asset string) (string, error) {
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// The second field carries a "*" marker in binary mode.
		if strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s lists no checksum for %s", ChecksumsAsset, asset)
}

func archiveName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("psl-%s-%s-%s%s", version, goos, goarch, ext)
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "psl.exe"
	}
	return "psl"
}
