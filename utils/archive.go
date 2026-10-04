package utils

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Archives use relative names, reject symlinks/special files and require output
// outside the source tree. Existing destinations are never overwritten.
func CreateZip(source, destination string) error {
	return archive(source, destination, func(out io.Writer, root string) error {
		w := zip.NewWriter(out)
		err := walkArchive(root, func(name string, info fs.FileInfo, file *os.File) error {
			h, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			h.Name = name
			if info.IsDir() {
				h.Name += "/"
			} else {
				h.Method = zip.Deflate
			}
			entry, err := w.CreateHeader(h)
			if err != nil {
				return err
			}
			if file != nil {
				_, err = io.Copy(entry, file)
			}
			return err
		})
		return errors.Join(err, w.Close())
	})
}
func CreateTarGz(source, destination string) error {
	return archive(source, destination, func(out io.Writer, root string) error {
		gz := gzip.NewWriter(out)
		w := tar.NewWriter(gz)
		err := walkArchive(root, func(name string, info fs.FileInfo, file *os.File) error {
			h, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			h.Name = name
			if err = w.WriteHeader(h); err != nil {
				return err
			}
			if file != nil {
				_, err = io.Copy(w, file)
			}
			return err
		})
		return errors.Join(err, w.Close(), gz.Close())
	})
}
func archive(source, destination string, write func(io.Writer, string) error) error {
	root, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	dst, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dst)
	if err != nil {
		return err
	}
	if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("utils: archive destination must be outside source")
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = write(f, root); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), dst)
}
func walkArchive(root string, fn func(string, fs.FileInfo, *os.File) error) error {
	return filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("utils: symlinks/special files are not archived")
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if name == "." {
			if info.IsDir() {
				return nil
			}
			name = e.Name()
		}
		name = filepath.ToSlash(name)
		if info.IsDir() {
			return fn(name, info, nil)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		opened, err := f.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, opened) {
			return errors.New("utils: source changed during archive")
		}
		return fn(name, opened, f)
	})
}
