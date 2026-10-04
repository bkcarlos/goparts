package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentRotationKeepsWholeRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := NewRotatingWriter(FileConfig{Path: path, MaxBytes: 28, Backups: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			record := fmt.Sprintf("id=%03d\n", i)
			n, err := w.Write([]byte(record))
			if err != nil || n != len(record) {
				t.Errorf("write=%d err=%v", n, err)
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > 28 || len(b)%7 != 0 {
			t.Fatalf("record split/size exceeded: %q", b)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			var id int
			if _, err := fmt.Sscanf(line, "id=%03d", &id); err != nil || id < 0 || id >= 100 || line != fmt.Sprintf("id=%03d", id) || seen[line] {
				t.Fatalf("invalid/duplicate record: %q", line)
			}
			seen[line] = true
		}
	}
	if len(seen) != 100 {
		t.Fatalf("lost records: got %d", len(seen))
	}
}

func TestRotationFailureClosesWriterAndPreservesOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := NewRotatingWriter(FileConfig{Path: path, MaxBytes: 3, Backups: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("old")); err != nil {
		t.Fatal(err)
	}
	// A directory at the backup path forces rename to fail on every supported OS.
	if err := os.Mkdir(path+".1", 0700); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("new")); err == nil || n != 0 {
		t.Fatalf("rotation failure hidden: %d %v", n, err)
	}
	if _, err := w.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("failed writer reused: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "old" {
		t.Fatalf("old log lost: %q %v", b, err)
	}
}

func TestOversizedRecordAndZeroBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := NewRotatingWriter(FileConfig{Path: path, MaxBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if n, err := w.Write([]byte("too big")); err == nil || n != 0 {
		t.Fatalf("oversized write accepted: %d %v", n, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "old" {
		t.Fatal("rejected record modified file")
	}
	if _, err := w.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil || string(b) != "new" {
		t.Fatalf("rotation=%q %v", b, err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("unexpected backups: %v %v", files, err)
	}
}
