package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// install extracts the new binary from a downloaded archive and puts it in
// place of target. The staged copy is written next to target so the final swap
// is a rename on the same filesystem, and target is restored if the swap fails
// halfway.
func install(archive, assetName, binary, target string) error {
	mode := os.FileMode(0o755)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(target)
	staged, err := os.CreateTemp(dir, ".psl-update-*")
	if err != nil {
		// Update checks this up front; reaching here means the permissions
		// changed underneath us.
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName)

	err = extract(archive, assetName, binary, staged)
	if closeErr := staged.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(stagedName, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", stagedName, err)
	}
	return swap(stagedName, target)
}

// swap moves the staged binary over target. The running executable is renamed
// aside first, because Windows will not let it be replaced directly.
func swap(staged, target string) error {
	backup := target + ".old"
	_ = os.Remove(backup)

	if err := os.Rename(target, backup); err != nil {
		return fmt.Errorf("cannot replace %s: %w (re-run: sudo psl update)", target, err)
	}
	if err := os.Rename(staged, target); err != nil {
		// Put the old executable back rather than leaving nothing behind.
		if restoreErr := os.Rename(backup, target); restoreErr != nil {
			return fmt.Errorf("install %s: %w (the previous executable is at %s)", target, err, backup)
		}
		return fmt.Errorf("install %s: %w", target, err)
	}
	// Deleting the running executable fails on Windows; it is only leftovers.
	_ = os.Remove(backup)
	return nil
}

// extract copies the named binary out of the archive into dst.
func extract(archive, assetName, binary string, dst io.Writer) error {
	if strings.HasSuffix(assetName, ".zip") {
		return extractZip(archive, binary, dst)
	}
	return extractTarGz(archive, binary, dst)
}

func extractTarGz(archive, binary string, dst io.Writer) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", archive, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", archive, err)
		}
		if header.Typeflag != tar.TypeReg || path.Base(header.Name) != binary {
			continue
		}
		if _, err := io.Copy(dst, io.LimitReader(tr, maxDownload)); err != nil {
			return fmt.Errorf("extract %s from %s: %w", binary, archive, err)
		}
		return nil
	}
	return fmt.Errorf("%s contains no %s", archive, binary)
}

func extractZip(archive, binary string, dst io.Writer) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("read %s: %w", archive, err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != binary {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("read %s from %s: %w", f.Name, archive, err)
		}
		_, err = io.Copy(dst, io.LimitReader(rc, maxDownload))
		rc.Close()
		if err != nil {
			return fmt.Errorf("extract %s from %s: %w", binary, archive, err)
		}
		return nil
	}
	return fmt.Errorf("%s contains no %s", archive, binary)
}
