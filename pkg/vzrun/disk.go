package vzrun

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/docker/go-units"
	"github.com/h2non/filetype"
	"github.com/h2non/filetype/matchers"
	"github.com/mac-vz/macvz/pkg/downloader"
	"github.com/mac-vz/macvz/pkg/iso9660util"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/yaml"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"path/filepath"
)

//EnsureDisk Creates. and verifies if the VM Disk are present
func EnsureDisk(ctx context.Context, cfg VM) error {
	kernelCompressed := filepath.Join(cfg.InstanceDir, filenames.KernelCompressed)
	initrd := filepath.Join(cfg.InstanceDir, filenames.Initrd)
	baseDisk := filepath.Join(cfg.InstanceDir, filenames.BaseDisk)
	BaseDiskZip := filepath.Join(cfg.InstanceDir, filenames.BaseDiskZip)

	if _, err := os.Stat(baseDisk); errors.Is(err, os.ErrNotExist) {
		var ensuredRequiredImages bool
		errs := make([]error, len(cfg.MacVZYaml.Images))
		resolveArch := yaml.ResolveArch()
		for i, f := range cfg.MacVZYaml.Images {
			if f.Arch != resolveArch {
				errs[i] = fmt.Errorf("image architecture %s didn't match system architecture: %s", f.Arch, resolveArch)
				continue
			}
			err := downloadImage(kernelCompressed, f.Kernel)
			if err != nil {
				errs[i] = fmt.Errorf("failed to download required images: %w", err)
				continue
			}
			err = downloadImage(initrd, f.Initram)
			if err != nil {
				errs[i] = fmt.Errorf("failed to download required images: %w", err)
				continue
			}
			err = downloadImage(BaseDiskZip, f.Base)
			if err != nil {
				errs[i] = fmt.Errorf("failed to download required images: %w", err)
				continue
			}
			fileName := ""
			if f.Arch == yaml.X8664 {
				fileName = "amd64"
			} else {
				fileName = "arm64"
			}
			err = iso9660util.Extract(BaseDiskZip, "focal-server-cloudimg-"+fileName+".img", baseDisk)
			if err != nil {
				errs[i] = fmt.Errorf("failed to extract base image: %w", err)
				continue
			}

			ensuredRequiredImages = true
			break
		}
		if !ensuredRequiredImages {
			return fmt.Errorf("failed to download the required images, attempted %d candidates, errors=%v",
				len(cfg.MacVZYaml.Images), errs)
		}

		err = uncompress(kernelCompressed, filepath.Join(cfg.InstanceDir, filenames.Kernel))
		if err != nil {
			logrus.Error("Error during uncompressing of kernel", err.Error())
		}
		inBytes, _ := units.RAMInBytes(*cfg.MacVZYaml.Disk)
		err := os.Truncate(baseDisk, inBytes)
		if err != nil {
			logrus.Error("Error during basedisk initial resize", err.Error())
			return err
		}
	}
	return nil
}

func downloadImage(disk string, remote string) error {
	res, err := downloader.Download(disk, remote,
		downloader.WithCache(),
	)
	if err != nil {
		return fmt.Errorf("failed to download %q: %w", remote, err)
	}
	switch res.Status {
	case downloader.StatusDownloaded:
		logrus.Infof("Downloaded image from %q", remote)
	case downloader.StatusUsedCache:
		logrus.Infof("Using cache %q", res.CachePath)
	default:
		logrus.Warnf("Unexpected result from downloader.Download(): %+v", res)
	}
	return nil
}

func isKernelUncompressed(filename string) (bool, error) {
	file, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer file.Close()

	buf := make([]byte, 2048)
	_, err = file.Read(buf)
	if err != nil {
		return false, err
	}
	kind, err := filetype.Match(buf)
	if err != nil {
		return false, err
	}
	// uncompressed ARM64 kernels are matched as a MS executable, which is
	// also an archive, so we need to special case it
	if kind == matchers.TypeExe {
		return true, nil
	}

	return false, nil
}

func uncompress(compressedFile string, targetFile string) error {
	uncompressed, err := isKernelUncompressed(compressedFile)
	if err != nil {
		logrus.Error("Error during uncompressing of kernel", err.Error())
		return err
	}

	gzipFile, err := os.Open(compressedFile)
	if err != nil {
		return err
	}

	writer, err := os.Create(targetFile)
	if err != nil {
		return err
	}
	defer writer.Close()

	if uncompressed {
		logrus.Println("Skipping uncompress of kernel...")
		reader, err := os.Open(compressedFile)
		if err != nil {
			return err
		}
		defer reader.Close()
		if _, err = io.Copy(writer, reader); err != nil {
			return err
		}
		return nil
	} else {
		logrus.Println("Trying to uncompress kernel...")
		reader, err := gzip.NewReader(gzipFile)
		if err != nil {
			return err
		}
		defer reader.Close()
		if _, err = io.Copy(writer, reader); err != nil {
			return err
		}
		return nil
	}
}
