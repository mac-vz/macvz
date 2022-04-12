package iso9660util

import (
	"archive/tar"
	"compress/gzip"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"io"
	"os"
	"path"
)

type Entry struct {
	Path   string
	Reader io.Reader
}

func Write(isoPath, label string, layout []Entry) error {
	if err := os.RemoveAll(isoPath); err != nil {
		return err
	}

	isoFile, err := os.Create(isoPath)
	if err != nil {
		return err
	}

	defer isoFile.Close()

	fs, err := iso9660.Create(isoFile, 0, 0, 0, "")
	if err != nil {
		return err
	}

	for _, f := range layout {
		if _, err := WriteFile(fs, f.Path, f.Reader); err != nil {
			return err
		}
	}

	finalizeOptions := iso9660.FinalizeOptions{
		RockRidge:        true,
		VolumeIdentifier: label,
	}
	if err := fs.Finalize(finalizeOptions); err != nil {
		return err
	}

	return isoFile.Close()
}

func WriteFile(fs filesystem.FileSystem, pathStr string, r io.Reader) (int64, error) {
	if dir := path.Dir(pathStr); dir != "" && dir != "/" {
		if err := fs.Mkdir(dir); err != nil {
			return 0, err
		}
	}
	f, err := fs.OpenFile(pathStr, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, r)
}

func IsISO9660(imagePath string) (bool, error) {
	imageFile, err := os.Open(imagePath)
	if err != nil {
		return false, err
	}
	defer imageFile.Close()

	fileInfo, err := imageFile.Stat()
	if err != nil {
		return false, err
	}
	_, err = iso9660.Read(imageFile, fileInfo.Size(), 0, 0)

	return err == nil, nil
}

func Extract(tarPath string, fileInTar string, outputPath string) error {
	tarFile, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	gzr, err := gzip.NewReader(tarFile)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		if header.Typeflag == tar.TypeReg && header.Name == fileInTar {
			f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}
