package filetree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFiltersAndSymlinks(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".hidden"), 0700)
	os.MkdirAll(filepath.Join(root, "visible"), 0700)
	for _, p := range []string{".hidden/x.go", "visible/a.go", "visible/b.txt"} {
		os.WriteFile(filepath.Join(root, p), nil, 0600)
	}
	paths, err := GetFileTreeFilesOnly(root, ExcludeHidden, FilterByExtension("go"))
	if err != nil || len(paths) != 1 || filepath.Base(paths[0]) != "a.go" {
		t.Fatal(paths, err)
	}
	os.Symlink(root, filepath.Join(root, "visible", "cycle"))
	if _, err = GetFileTree(root); err != nil {
		t.Fatal(err)
	}
}
