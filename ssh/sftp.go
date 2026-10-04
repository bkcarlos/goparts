package ssh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/pkg/sftp"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Progress func(written, total int64) error
type progressReader struct {
	ctx            context.Context
	r              io.Reader
	written, total int64
	notify         Progress
}

func (r *progressReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	r.written += int64(n)
	if n > 0 && r.notify != nil {
		if e := r.notify(r.written, r.total); e != nil {
			return n, e
		}
	}
	return n, err
}
func (c *Client) withSFTP(ctx context.Context, fn func(*sftp.Client) error) error {
	if ctx == nil {
		return errors.New("ssh: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stopConnection := context.AfterFunc(ctx, func() { c.conn.Close() })
	sf, err := sftp.NewClient(c.conn)
	if err != nil {
		stopConnection()
		return err
	}
	if !stopConnection() {
		sf.Close()
		return ctx.Err()
	}
	defer sf.Close()
	stop := context.AfterFunc(ctx, func() { sf.Close() })
	defer stop()
	err = fn(sf)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (c *Client) TransferFileWithProgress(ctx context.Context, local, remote string, progress Progress) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > c.cfg.MaxFileBytes {
		return errors.New("ssh: invalid/oversized source")
	}
	return c.withSFTP(ctx, func(sf *sftp.Client) error {
		var suffix [12]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return err
		}
		temp := path.Join(path.Dir(remote), ".goparts-"+hex.EncodeToString(suffix[:]))
		out, err := sf.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
		if err != nil {
			return err
		}
		defer sf.Remove(temp)
		defer out.Close()
		if err = out.Chmod(0600); err != nil {
			return err
		}
		n, err := io.Copy(out, &progressReader{ctx: ctx, r: io.LimitReader(f, info.Size()+1), total: info.Size(), notify: progress})
		if err != nil {
			return err
		}
		if n != info.Size() {
			return errors.New("ssh: source size changed")
		}
		if err = out.Close(); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		return sf.Rename(temp, remote)
	})
}
func (c *Client) DownloadFileWithProgress(ctx context.Context, remote, local string, progress Progress) error {
	return c.withSFTP(ctx, func(sf *sftp.Client) error { return c.download(ctx, sf, remote, local, progress) })
}
func (c *Client) download(ctx context.Context, sf *sftp.Client, remote, local string, progress Progress) error {
	info, err := sf.Lstat(remote)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > c.cfg.MaxFileBytes {
		return errors.New("ssh: invalid/oversized remote file")
	}
	source, err := sf.Open(remote)
	if err != nil {
		return err
	}
	defer source.Close()
	out, err := os.CreateTemp(filepath.Dir(local), ".sftp-*")
	if err != nil {
		return err
	}
	defer out.Close()
	defer os.Remove(out.Name())
	n, err := io.Copy(out, &progressReader{ctx: ctx, r: io.LimitReader(source, info.Size()+1), total: info.Size(), notify: progress})
	if err != nil {
		return err
	}
	if n != info.Size() {
		return errors.New("ssh: remote size changed")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Link(out.Name(), local)
}

// DownloadDirectory does not follow remote links and refuses local symlink directories.
func (c *Client) DownloadDirectory(ctx context.Context, remote, local string, progress Progress) error {
	return c.withSFTP(ctx, func(sf *sftp.Client) error {
		info, err := sf.Lstat(remote)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("ssh: remote directory required")
		}
		if err = os.MkdirAll(local, 0700); err != nil {
			return err
		}
		rootInfo, err := os.Lstat(local)
		if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("ssh: unsafe destination directory")
		}
		walker := sf.Walk(remote)
		for walker.Step() {
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = walker.Err(); err != nil {
				return err
			}
			entry := walker.Stat()
			rel := strings.TrimPrefix(strings.TrimPrefix(walker.Path(), strings.TrimSuffix(remote, "/")), "/")
			if rel == "" {
				continue
			}
			if !filepath.IsLocal(filepath.FromSlash(rel)) || strings.Contains(rel, "\\") {
				return errors.New("ssh: unsafe remote path")
			}
			target := filepath.Join(local, filepath.FromSlash(rel))
			if entry.Mode()&os.ModeSymlink != 0 {
				return errors.New("ssh: remote symlink rejected")
			}
			if entry.IsDir() {
				if err = os.Mkdir(target, 0700); err != nil && !os.IsExist(err) {
					return err
				}
				info, err := os.Lstat(target)
				if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("ssh: unsafe local directory")
				}
			} else {
				if err = c.download(ctx, sf, walker.Path(), target, progress); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (c *Client) ResolveSymlink(ctx context.Context, remote string) (string, error) {
	var result string
	err := c.withSFTP(ctx, func(sf *sftp.Client) error { var err error; result, err = sf.RealPath(remote); return err })
	return result, err
}
func (c *Client) CheckDiskSpace(ctx context.Context, remote string, required uint64) error {
	return c.withSFTP(ctx, func(sf *sftp.Client) error {
		stat, err := sf.StatVFS(remote)
		if err != nil {
			return err
		}
		available := stat.Bavail * stat.Frsize
		if stat.Frsize > 0 && available/stat.Frsize != stat.Bavail {
			return nil
		}
		if available < required {
			return errors.New("ssh: insufficient remote disk space")
		}
		return nil
	})
}
