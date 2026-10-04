package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryUploadAndMD5(t *testing.T) {
	backend := &memoryBackend{data: map[string]string{}}
	client, _ := New(backend, Config{BasePath: "prefix"})
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0700)
	p := filepath.Join(root, "sub", "file")
	os.WriteFile(p, []byte("abc"), 0600)
	objects, err := client.UploadDirectory(context.Background(), "dir", root, DirectoryOptions{Workers: 1})
	if err != nil || len(objects) != 1 || backend.data["prefix/dir/sub/file"] != "abc" {
		t.Fatal(objects, err)
	}
	digest, err := ComputeFileMD5(p)
	if err != nil || digest != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatal(digest, err)
	}
	os.Symlink(p, filepath.Join(root, "link"))
	if _, err = client.UploadDirectory(context.Background(), "dir", root, DirectoryOptions{Workers: 1}); err == nil {
		t.Fatal("symlink followed")
	}
}
