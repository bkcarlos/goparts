package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T) (SSHConfig, func()) {
	t.Helper()
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromKey(private)
	config := &gossh.ServerConfig{PasswordCallback: func(_ gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		if string(password) != "secret" {
			return nil, os.ErrPermission
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, raw)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn, channels, requests, err := gossh.NewServerConn(raw, config)
				if err != nil {
					raw.Close()
					return
				}
				defer conn.Close()
				go gossh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						channel.Reject(gossh.UnknownChannelType, "unsupported")
						continue
					}
					ch, reqs, err := channel.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer ch.Close()
						for req := range reqs {
							switch req.Type {
							case "subsystem":
								var payload struct{ Name string }
								gossh.Unmarshal(req.Payload, &payload)
								req.Reply(payload.Name == "sftp", nil)
								if payload.Name == "sftp" {
									server, err := sftp.NewServer(ch)
									if err == nil {
										server.Serve()
										server.Close()
									}
									return
								}
							case "exec":
								var payload struct{ Command string }
								gossh.Unmarshal(req.Payload, &payload)
								req.Reply(true, nil)
								if payload.Command == "wait" {
									io.Copy(io.Discard, ch)
									return
								}
								if payload.Command == "hello" {
									io.WriteString(ch, "world")
								}
								status := make([]byte, 4)
								binary.BigEndian.PutUint32(status, 0)
								ch.SendRequest("exit-status", false, status)
								return
							default:
								req.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	cleanup := func() {
		listener.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	}
	return SSHConfig{Hosts: []string{listener.Addr().String()}, User: "test", Auth: []gossh.AuthMethod{Password("secret")}, HostKeyCallback: gossh.FixedHostKey(signer.PublicKey()), Timeout: time.Second}, cleanup
}
func TestCommandsSFTPAndPool(t *testing.T) {
	cfg, closeServer := testServer(t)
	defer closeServer()
	cfg.Hosts = append([]string{"127.0.0.1:1"}, cfg.Hosts...)
	c, err := NewSSHClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	result, err := c.ExecuteCommand(context.Background(), "hello")
	if err != nil || result.Stdout != "world" {
		t.Fatal(result, err)
	}
	root := t.TempDir()
	local := filepath.Join(root, "input")
	remote := filepath.Join(root, "remote")
	os.WriteFile(local, []byte("payload"), 0600)
	if err = c.TransferFileWithProgress(context.Background(), local, remote, nil); err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(root, "output")
	if err = c.DownloadFileWithProgress(context.Background(), remote, downloaded, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(downloaded)
	if string(b) != "payload" {
		t.Fatal(string(b))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = c.ExecuteCommand(ctx, "wait"); err == nil {
		t.Fatal("cancellation ignored")
	}
	pool, _ := NewPool(cfg, 1)
	defer pool.Close()
	a, release, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release(false)
	bclient, release2, err := pool.Acquire(context.Background())
	if err != nil || a != bclient {
		t.Fatal("not reused", err)
	}
	release2(false)
}
func TestHostVerificationAndQuoting(t *testing.T) {
	cfg, closeServer := testServer(t)
	defer closeServer()
	cfg.HostKeyCallback = nil
	if _, err := NewSSHClient(context.Background(), cfg); err == nil {
		t.Fatal("unverified host")
	}
	if !strings.Contains(shellQuote("a'b"), "'\"'\"'") {
		t.Fatal("quote")
	}
}
