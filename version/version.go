// Package version publishes immutable release manifests and verified updates.
package version

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("version: object not found")
var ErrChecksum = errors.New("version: checksum mismatch")

// StorageProvider abstracts any object store. Put must replace one key atomically;
// Open must translate absence into ErrNotFound. Returned readers are owned here.
type StorageProvider interface {
	Put(context.Context, string, io.Reader, int64) error
	Open(context.Context, string) (io.ReadCloser, error)
}
type StorageAdapter struct {
	PutFunc  func(context.Context, string, io.Reader, int64) error
	OpenFunc func(context.Context, string) (io.ReadCloser, error)
}

func (s StorageAdapter) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	if s.PutFunc == nil {
		return errors.New("version: missing PutFunc")
	}
	return s.PutFunc(ctx, key, r, n)
}
func (s StorageAdapter) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.OpenFunc == nil {
		return nil, errors.New("version: missing OpenFunc")
	}
	return s.OpenFunc(ctx, key)
}

type ReleaseFile struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	MD5      string `json:"md5,omitempty"`
}
type ReleaseInfo struct {
	Version   string        `json:"version"`
	BuildTime time.Time     `json:"build_time"`
	CommitID  string        `json:"commit_id"`
	Files     []ReleaseFile `json:"files"`
}
type ReleaseIndex struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
}
type BinFileInfo struct{ Path, Name, Platform, Arch string }
type UpdateInfo struct {
	CurrentVersion string
	Release        ReleaseInfo
	Available      bool
}
type Publisher struct {
	Store   StorageProvider
	Workers int
	mu      sync.Mutex
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validID(s string) bool       { return identifier.MatchString(s) && s != "." && s != ".." }
func manifestKey(v string) string { return "releases/" + v + "/release.json" }
func readJSON(ctx context.Context, store StorageProvider, key string, out any) error {
	r, err := store.Open(ctx, key)
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 4<<20 {
		return errors.New("version: manifest exceeds limit")
	}
	if err = json.Unmarshal(b, out); err != nil {
		return errors.New("version: invalid manifest")
	}
	return nil
}
func putJSON(ctx context.Context, store StorageProvider, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return store.Put(ctx, key, strings.NewReader(string(b)), int64(len(b)))
}

// UploadRelease publishes latest only after files, release manifest and index
// succeed. One publisher must own a prefix; distributed writers need an external lock.
func (p *Publisher) UploadRelease(ctx context.Context, version string, buildTime time.Time, commitID string, files []BinFileInfo) (ReleaseInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ctx == nil || p.Store == nil || !validID(version) {
		return ReleaseInfo{}, errors.New("version: invalid publisher/version")
	}
	var existing ReleaseInfo
	err := readJSON(ctx, p.Store, manifestKey(version), &existing)
	if err == nil {
		return ReleaseInfo{}, errors.New("version: release already exists")
	}
	if !errors.Is(err, ErrNotFound) {
		return ReleaseInfo{}, err
	}
	uploaded, err := p.UploadBinFiles(ctx, version, files)
	if err != nil {
		return ReleaseInfo{}, err
	}
	release := ReleaseInfo{version, buildTime, commitID, uploaded}
	if err = putJSON(ctx, p.Store, manifestKey(version), release); err != nil {
		return release, err
	}
	var index ReleaseIndex
	if err = readJSON(ctx, p.Store, "index.json", &index); err != nil && !errors.Is(err, ErrNotFound) {
		return release, err
	}
	index.Latest = version
	index.Versions = append(index.Versions, version)
	if err = putJSON(ctx, p.Store, "index.json", index); err != nil {
		return release, err
	}
	return release, putJSON(ctx, p.Store, "latest.json", map[string]string{"version": version})
}
func (p *Publisher) UploadBinFiles(ctx context.Context, version string, files []BinFileInfo) ([]ReleaseFile, error) {
	if ctx == nil || p.Store == nil || !validID(version) || len(files) == 0 || p.Workers < 0 || p.Workers > 64 {
		return nil, errors.New("version: invalid upload configuration")
	}
	workers := p.Workers
	if workers == 0 {
		workers = 4
	}
	seen := map[string]bool{}
	inputs := append([]BinFileInfo(nil), files...)
	for i := range inputs {
		f := &inputs[i]
		if f.Name == "" {
			f.Name = filepath.Base(f.Path)
		}
		if f.Platform == "" {
			f.Platform = runtime.GOOS
		}
		if f.Arch == "" {
			f.Arch = runtime.GOARCH
		}
		key := f.Platform + "/" + f.Arch + "/" + f.Name
		if !validID(f.Name) || !validID(f.Platform) || !validID(f.Arch) || seen[key] {
			return nil, errors.New("version: invalid/duplicate binary")
		}
		seen[key] = true
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	out := make([]ReleaseFile, len(inputs))
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f := inputs[i]
				info, err := p.upload(ctx, version, f)
				if err != nil {
					errs <- err
					cancel()
					return
				}
				out[i] = info
			}
		}()
	}
send:
	for i := range inputs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (p *Publisher) upload(ctx context.Context, version string, input BinFileInfo) (ReleaseFile, error) {
	f, err := os.Open(input.Path)
	if err != nil {
		return ReleaseFile{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReleaseFile{}, err
	}
	if !info.Mode().IsRegular() {
		return ReleaseFile{}, errors.New("version: regular binary required")
	}
	sha, md := sha256.New(), md5.New()
	key := "releases/" + version + "/" + input.Platform + "/" + input.Arch + "/" + input.Name
	count := &countReader{r: io.TeeReader(f, io.MultiWriter(sha, md))}
	if err = p.Store.Put(ctx, key, count, info.Size()); err != nil {
		return ReleaseFile{}, err
	}
	if count.n != info.Size() {
		return ReleaseFile{}, errors.New("version: provider did not consume complete binary")
	}
	return ReleaseFile{input.Name, key, input.Platform, input.Arch, info.Size(), hex.EncodeToString(sha.Sum(nil)), hex.EncodeToString(md.Sum(nil))}, nil
}

type countReader struct {
	r io.Reader
	n int64
}

func (r *countReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

type Updater struct {
	Store    StorageProvider
	MaxBytes int64
}

func (u *Updater) GetLatestVersion(ctx context.Context) (ReleaseInfo, error) {
	if ctx == nil || u.Store == nil {
		return ReleaseInfo{}, errors.New("version: context/store required")
	}
	var latest struct {
		Version string `json:"version"`
	}
	if err := readJSON(ctx, u.Store, "latest.json", &latest); err != nil {
		return ReleaseInfo{}, err
	}
	return u.Release(ctx, latest.Version)
}
func (u *Updater) Release(ctx context.Context, v string) (ReleaseInfo, error) {
	if ctx == nil || u.Store == nil || !validID(v) {
		return ReleaseInfo{}, errors.New("version: invalid release")
	}
	var release ReleaseInfo
	if err := readJSON(ctx, u.Store, manifestKey(v), &release); err != nil {
		return release, err
	}
	if release.Version != v || len(release.Files) == 0 {
		return release, errors.New("version: invalid release manifest")
	}
	for _, f := range release.Files {
		if !validID(f.Name) || !validID(f.Platform) || !validID(f.Arch) || f.Key != "releases/"+v+"/"+f.Platform+"/"+f.Arch+"/"+f.Name || f.Size < 0 {
			return release, errors.New("version: unsafe release file")
		}
	}
	return release, nil
}

// CheckForUpdate compares opaque release IDs; publishing order defines latest.
func (u *Updater) CheckForUpdate(ctx context.Context, current string) (UpdateInfo, error) {
	r, err := u.GetLatestVersion(ctx)
	return UpdateInfo{current, r, r.Version != "" && r.Version != current}, err
}
func (u *Updater) DownloadPlatform(ctx context.Context, v, platform, arch, destination string) (ReleaseFile, error) {
	r, err := u.Release(ctx, v)
	if err != nil {
		return ReleaseFile{}, err
	}
	if platform == "" {
		platform = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	var selected *ReleaseFile
	for i := range r.Files {
		f := &r.Files[i]
		if f.Platform == platform && f.Arch == arch {
			if selected != nil {
				return ReleaseFile{}, errors.New("version: multiple platform binaries; select a file explicitly")
			}
			selected = f
		}
	}
	if selected == nil {
		return ReleaseFile{}, ErrNotFound
	}
	return *selected, u.DownloadFile(ctx, *selected, destination)
}
func (u *Updater) DownloadFile(ctx context.Context, file ReleaseFile, destination string) error {
	if ctx == nil || u.Store == nil || destination == "" || file.Size < 0 || u.MaxBytes < 0 || u.MaxBytes > 1<<50 {
		return errors.New("version: invalid download")
	}
	limit := u.MaxBytes
	if limit == 0 {
		limit = 2 << 30
	}
	if file.Size > limit {
		return errors.New("version: binary exceeds limit")
	}
	digest := file.SHA256
	expectedLen := 32
	if digest == "" {
		digest = file.MD5
		expectedLen = 16
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != expectedLen {
		return errors.New("version: valid checksum required")
	}
	r, err := u.Store.Open(ctx, file.Key)
	if err != nil {
		return err
	}
	defer r.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	sha, md := sha256.New(), md5.New()
	n, err := io.Copy(io.MultiWriter(tmp, sha, md), io.LimitReader(r, file.Size+1))
	if err != nil {
		return err
	}
	if n != file.Size {
		return errors.New("version: size mismatch")
	}
	actual := hex.EncodeToString(sha.Sum(nil))
	if expectedLen == 16 {
		actual = hex.EncodeToString(md.Sum(nil))
	}
	if !strings.EqualFold(actual, digest) {
		return ErrChecksum
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Link(tmp.Name(), destination)
}

// Update preserves target.bak and replaces the target only after verification.
// Existing backups cause an error. On Windows a running executable may be locked;
// return that error and perform replacement from a separate launcher.
func (u *Updater) Update(ctx context.Context, current, target string) (UpdateInfo, error) {
	info, err := u.CheckForUpdate(ctx, current)
	if err != nil || !info.Available {
		return info, err
	}
	stat, err := os.Lstat(target)
	if err != nil {
		return info, err
	}
	if !stat.Mode().IsRegular() {
		return info, errors.New("version: target must be regular")
	}
	tempDir, err := os.MkdirTemp(filepath.Dir(target), ".release-*")
	if err != nil {
		return info, err
	}
	defer os.RemoveAll(tempDir)
	replacement := filepath.Join(tempDir, "binary")
	if _, err = u.DownloadPlatform(ctx, info.Release.Version, "", "", replacement); err != nil {
		return info, err
	}
	if err = os.Chmod(replacement, stat.Mode().Perm()); err != nil {
		return info, err
	}
	backup := target + ".bak"
	if err = os.Link(target, backup); err != nil {
		return info, fmt.Errorf("version: preserve backup: %w", err)
	}
	if err = ctx.Err(); err != nil {
		os.Remove(backup)
		return info, err
	}
	if err = os.Rename(replacement, target); err != nil {
		os.Remove(backup)
		return info, err
	}
	return info, nil
}
func Rollback(target string) error {
	backup := target + ".bak"
	info, err := os.Lstat(backup)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("version: backup must be regular")
	}
	return os.Rename(backup, target)
}
