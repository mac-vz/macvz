package downloader

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"gotest.tools/v3/assert"
)

const (
	dummyRemoteFileDigest = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" //sha256 for test
)

func startDummyServer() *httptest.Server {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("test"))
		if err != nil {
			return
		}
	}))
	return svr
}

func TestDownloadRemote(t *testing.T) {
	server := startDummyServer()

	dummyRemoteFileURL := server.URL
	if testing.Short() {
		t.Skip()
	}
	t.Run("without cache", func(t *testing.T) {
		t.Run("without digest", func(t *testing.T) {
			localPath := filepath.Join(t.TempDir(), t.Name())
			r, err := Download(localPath, dummyRemoteFileURL)
			assert.NilError(t, err)
			assert.Equal(t, StatusDownloaded, r.Status)

			// download again, make sure StatusSkippedIsReturned
			r, err = Download(localPath, dummyRemoteFileURL)
			assert.NilError(t, err)
			assert.Equal(t, StatusSkipped, r.Status)
		})
		t.Run("with digest", func(t *testing.T) {
			wrongDigest := digest.Digest("sha256:8313944efb4f38570c689813f288058b674ea6c487017a5a4738dc674b65f9d9")
			localPath := filepath.Join(t.TempDir(), t.Name())
			_, err := Download(localPath, dummyRemoteFileURL, WithExpectedDigest(wrongDigest))
			assert.ErrorContains(t, err, "expected digest")

			r, err := Download(localPath, dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest))
			assert.NilError(t, err)
			assert.Equal(t, StatusDownloaded, r.Status)

			r, err = Download(localPath, dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest))
			assert.NilError(t, err)
			assert.Equal(t, StatusSkipped, r.Status)
		})
	})
	t.Run("with cache", func(t *testing.T) {
		cacheDir := filepath.Join(t.TempDir(), "cache")
		localPath := filepath.Join(t.TempDir(), t.Name())
		r, err := Download(localPath, dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest), WithCacheDir(cacheDir))
		assert.NilError(t, err)
		assert.Equal(t, StatusDownloaded, r.Status)

		r, err = Download(localPath, dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest), WithCacheDir(cacheDir))
		assert.NilError(t, err)
		assert.Equal(t, StatusSkipped, r.Status)

		localPath2 := localPath + "-2"
		r, err = Download(localPath2, dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest), WithCacheDir(cacheDir))
		assert.NilError(t, err)
		assert.Equal(t, StatusUsedCache, r.Status)
	})
	t.Run("caching-only mode", func(t *testing.T) {
		_, err := Download("", dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest))
		assert.ErrorContains(t, err, "cache directory to be specified")

		cacheDir := filepath.Join(t.TempDir(), "cache")
		r, err := Download("", dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest), WithCacheDir(cacheDir))
		assert.NilError(t, err)
		assert.Equal(t, StatusDownloaded, r.Status)

		r, err = Download("", dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest), WithCacheDir(cacheDir))
		assert.NilError(t, err)
		assert.Equal(t, StatusUsedCache, r.Status)

		localPath := filepath.Join(t.TempDir(), t.Name())
		r, err = Download(localPath, dummyRemoteFileURL, WithExpectedDigest(dummyRemoteFileDigest), WithCacheDir(cacheDir))
		assert.NilError(t, err)
		assert.Equal(t, StatusUsedCache, r.Status)
	})
}
