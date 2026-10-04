package utils

import (
	"archive/zip"
	"github.com/bkcarlos/goparts/utils/diskspace"
	"os"
	"path/filepath"
	"testing"
)

func TestArchivesAndSpace(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0700)
	os.WriteFile(filepath.Join(root, "sub", "file"), []byte("content"), 0600)
	dst := filepath.Join(t.TempDir(), "files.zip")
	if err := CreateZip(root, dst); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 2 || z.File[1].Name != "sub/file" {
		t.Fatal(z.File)
	}
	if err = CreateZip(root, dst); err == nil {
		t.Fatal("overwrote destination")
	}
	if err = CreateTarGz(root, filepath.Join(t.TempDir(), "files.tgz")); err != nil {
		t.Fatal(err)
	}
	os.Symlink("sub/file", filepath.Join(root, "link"))
	if err = CreateZip(root, filepath.Join(t.TempDir(), "bad.zip")); err == nil {
		t.Fatal("followed symlink")
	}
	if err = diskspace.CheckDiskSpace(root, 1); err != nil {
		t.Fatal(err)
	}
}
