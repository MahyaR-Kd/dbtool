package telegram

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// tarDir bundles every regular file in dir into a single tar archive written
// to a new temp file. mydumper already gzip-compresses each per-table file
// individually, so the tar itself is left uncompressed. It returns the path
// to the temp tar file and its size; the caller is responsible for removing
// the file once it is no longer needed.
func tarDir(dir string) (tarPath string, size int64, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, fmt.Errorf("read dump dir: %w", err)
	}

	out, err := os.CreateTemp("", "dbtool-telegram-*.tar")
	if err != nil {
		return "", 0, fmt.Errorf("create temp tar: %w", err)
	}
	tarPath = out.Name()

	if err := writeTar(out, dir, entries); err != nil {
		out.Close()
		os.Remove(tarPath)
		return "", 0, err
	}

	if err := out.Close(); err != nil {
		os.Remove(tarPath)
		return "", 0, fmt.Errorf("close temp tar: %w", err)
	}

	info, err := os.Stat(tarPath)
	if err != nil {
		os.Remove(tarPath)
		return "", 0, fmt.Errorf("stat temp tar: %w", err)
	}

	return tarPath, info.Size(), nil
}

func writeTar(w io.Writer, dir string, entries []os.DirEntry) error {
	tw := tar.NewWriter(w)

	for _, e := range entries {
		if e.IsDir() {
			continue // dump directories are flat; skip nested dirs defensively
		}

		info, err := e.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", e.Name(), err)
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("build tar header for %s: %w", e.Name(), err)
		}
		hdr.Name = e.Name()

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header for %s: %w", e.Name(), err)
		}

		if err := copyFileInto(tw, filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("write %s into tar: %w", e.Name(), err)
		}
	}

	return tw.Close()
}

func copyFileInto(w io.Writer, path string) error {
	f, err := os.Open(path) // #nosec G304 -- path is built from a directory listing of a dump dir we created ourselves
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}
