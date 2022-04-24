package vzrun

import (
	"context"
	"errors"
	"fmt"
	"github.com/docker/go-units"
	"github.com/mac-vz/macvz/pkg/downloader"
	"github.com/mac-vz/macvz/pkg/iso9660util"
	"github.com/mac-vz/macvz/pkg/store/filenames"
	"github.com/mac-vz/macvz/pkg/yaml"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
)

//EnsureDisk Creates. and verifies if the VM Disk are present
func EnsureDisk(ctx context.Context, cfg VM) error {
	kernel := filepath.Join(cfg.InstanceDir, filenames.Kernel)
	initrd := filepath.Join(cfg.InstanceDir, filenames.Initrd)
	baseDisk := filepath.Join(cfg.InstanceDir, filenames.BaseDisk)
	BaseDiskZip := filepath.Join(cfg.InstanceDir, filenames.BaseDiskZip)

	if _, err := os.Stat(baseDisk); errors.Is(err, os.ErrNotExist) {
		var ensuredRequiredImages bool
		errs := make([]error, len(cfg.MacVZYaml.Images))
		for i, f := range cfg.MacVZYaml.Images {
			err := downloadImage(kernel, f.Kernel)
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
			}

			ensuredRequiredImages = true
			break
		}
		if !ensuredRequiredImages {
			return fmt.Errorf("failed to download the required images, attempted %d candidates, errors=%v",
				len(cfg.MacVZYaml.Images), errs)
		}

		inBytes, _ := units.RAMInBytes(*cfg.MacVZYaml.Disk)
		err := os.Truncate(baseDisk, inBytes)
		if err != nil {
			logrus.Println("Error during basedisk initial resize", err.Error())
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
