package downloader

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/balaji113/macvz/pkg/localpathutil"
	"github.com/cheggaaa/pb/v3"
	"github.com/containerd/continuity/fs"
	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
)

type Status = string

const (
	StatusDownloaded Status = "downloaded"
	StatusSkipped    Status = "skipped"
	StatusUsedCache  Status = "used-cache"
)

type Result struct {
	Status          Status
	CachePath       string // "/Users/foo/Library/Caches/lima/download/by-url-sha256/<SHA256_OF_URL>/data"
	ValidatedDigest bool
}

type options struct {
	cacheDir string // default: empty (disables caching)
}

type Opt func(*options) error

// WithCache enables caching using filepath.Join(os.UserCacheDir(), "lima") as the cache dir.
func WithCache() Opt {
	return func(o *options) error {
		ucd, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		cacheDir := filepath.Join(ucd, "lima")
		return WithCacheDir(cacheDir)(o)
	}
}

// WithCacheDir enables caching using the specified dir.
// Empty value disables caching.
func WithCacheDir(cacheDir string) Opt {
	return func(o *options) error {
		o.cacheDir = cacheDir
		return nil
	}
}

// Download downloads the remote resource into the local path.
//
// Download caches the remote resource if WithCache or WithCacheDir option is specified.
// Local files are not cached.
//
// When the local path already exists, Download returns Result with StatusSkipped.
// (So, the local path cannot be set to /dev/null for "caching only" mode.)
//
// The local path can be an empty string for "caching only" mode.
func Download(local, remote string, opts ...Opt) (*Result, error) {
	var o options
	for _, f := range opts {
		if err := f(&o); err != nil {
			return nil, err
		}
	}
	var localPath string
	if local == "" {
		if o.cacheDir == "" {
			return nil, fmt.Errorf("caching-only mode requires the cache directory to be specified")
		}
	} else {
		var err error
		localPath, err = canonicalLocalPath(local)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(localPath); err == nil {
			logrus.Debugf("file %q already exists, skipping downloading from %q (and skipping digest validation)", localPath, remote)
			res := &Result{
				Status:          StatusSkipped,
				ValidatedDigest: false,
			}
			return res, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		localPathDir := filepath.Dir(localPath)
		if err := os.MkdirAll(localPathDir, 0755); err != nil {
			return nil, err
		}
	}

	if IsLocal(remote) {
		if err := copyLocal(localPath, remote); err != nil {
			return nil, err
		}
		res := &Result{
			Status:          StatusDownloaded,
			ValidatedDigest: false,
		}
		return res, nil
	}

	if o.cacheDir == "" {
		if err := downloadHTTP(localPath, remote); err != nil {
			return nil, err
		}
		res := &Result{
			Status:          StatusDownloaded,
			ValidatedDigest: false,
		}
		return res, nil
	}

	shad := filepath.Join(o.cacheDir, "download", "by-url-sha256", fmt.Sprintf("%x", sha256.Sum256([]byte(remote))))
	shadData := filepath.Join(shad, "data")
	if _, err := os.Stat(shadData); err == nil {
		logrus.Debugf("file %q is cached as %q", localPath, shadData)
		if err := copyLocal(localPath, shadData); err != nil {
			return nil, err
		}

		res := &Result{
			Status:          StatusUsedCache,
			CachePath:       shadData,
			ValidatedDigest: false,
		}
		return res, nil
	}
	if err := os.RemoveAll(shad); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(shad, 0700); err != nil {
		return nil, err
	}
	shadURL := filepath.Join(shad, "url")
	if err := os.WriteFile(shadURL, []byte(remote), 0644); err != nil {
		return nil, err
	}
	if err := downloadHTTP(shadData, remote); err != nil {
		return nil, err
	}
	// no need to pass the digest to copyLocal(), as we already verified the digest
	if err := copyLocal(localPath, shadData); err != nil {
		return nil, err
	}
	res := &Result{
		Status:          StatusDownloaded,
		CachePath:       shadData,
		ValidatedDigest: false,
	}
	return res, nil
}

func IsLocal(s string) bool {
	return !strings.Contains(s, "://") || strings.HasPrefix(s, "file://")
}

// canonicalLocalPath canonicalizes the local path string.
// - Make sure the file has no scheme, or the `file://` scheme
// - If it has the `file://` scheme, strip the scheme and make sure the filename is absolute
// - Expand a leading `~`, or convert relative to absolute name
func canonicalLocalPath(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("got empty path")
	}
	if !IsLocal(s) {
		return "", fmt.Errorf("got non-local path: %q", s)
	}
	if strings.HasPrefix(s, "file://") {
		res := strings.TrimPrefix(s, "file://")
		if !filepath.IsAbs(res) {
			return "", fmt.Errorf("got non-absolute path %q", res)
		}
		return res, nil
	}
	return localpathutil.Expand(s)
}

func copyLocal(dst, src string) error {
	srcPath, err := canonicalLocalPath(src)
	if err != nil {
		return err
	}
	if dst == "" {
		// empty dst means caching-only mode
		return nil
	}
	dstPath, err := canonicalLocalPath(dst)
	if err != nil {
		return err
	}
	return fs.CopyFile(dstPath, srcPath)
}

func createBar(size int64) (*pb.ProgressBar, error) {
	bar := pb.New64(size)

	bar.Set(pb.Bytes, true)
	if isatty.IsTerminal(os.Stdout.Fd()) {
		bar.SetTemplateString(`{{counters . }} {{bar . | green }} {{percent .}} {{speed . "%s/s"}}`)
		bar.SetRefreshRate(200 * time.Millisecond)
	} else {
		bar.Set(pb.Terminal, false)
		bar.Set(pb.ReturnSymbol, "\n")
		bar.SetTemplateString(`{{counters . }} ({{percent .}}) {{speed . "%s/s"}}`)
		bar.SetRefreshRate(5 * time.Second)
	}
	bar.SetWidth(80)
	if err := bar.Err(); err != nil {
		return nil, err
	}

	return bar, nil
}

func downloadHTTP(localPath, url string) error {
	if localPath == "" {
		return fmt.Errorf("downloadHTTP: got empty localPath")
	}
	logrus.Debugf("downloading %q into %q", url, localPath)
	localPathTmp := localPath + ".tmp"
	if err := os.RemoveAll(localPathTmp); err != nil {
		return err
	}
	fileWriter, err := os.Create(localPathTmp)
	if err != nil {
		return err
	}
	defer fileWriter.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected HTTP status %d, got %s", http.StatusOK, resp.Status)
	}
	bar, err := createBar(resp.ContentLength)
	if err != nil {
		return err
	}

	writers := []io.Writer{fileWriter}
	multiWriter := io.MultiWriter(writers...)

	bar.Start()
	if _, err := io.Copy(multiWriter, bar.NewProxyReader(resp.Body)); err != nil {
		return err
	}
	bar.Finish()

	if err := fileWriter.Sync(); err != nil {
		return err
	}
	if err := fileWriter.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(localPath); err != nil {
		return err
	}
	if err := os.Rename(localPathTmp, localPath); err != nil {
		return err
	}

	return nil
}
