// Package ssh provides verified SSH connections, bounded commands and SFTP.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type SSHConfig struct {
	Hosts           []string
	User            string
	Auth            []gossh.AuthMethod
	HostKeyCallback gossh.HostKeyCallback
	KnownHostsFile  string
	Timeout         time.Duration
	MaxOutputBytes  int
	MaxFileBytes    int64
}

func Password(password string) gossh.AuthMethod { return gossh.Password(password) }
func PrivateKey(key, passphrase []byte) (gossh.AuthMethod, error) {
	var signer gossh.Signer
	var err error
	if len(passphrase) > 0 {
		signer, err = gossh.ParsePrivateKeyWithPassphrase(key, passphrase)
	} else {
		signer, err = gossh.ParsePrivateKey(key)
	}
	if err != nil {
		return nil, errors.New("ssh: invalid private key/passphrase")
	}
	return gossh.PublicKeys(signer), nil
}

// Agent connects to SSH_AUTH_SOCK (or the supplied socket); caller closes the
// returned connection only after all authentication using it has completed.
func Agent(ctx context.Context, socket string) (gossh.AuthMethod, io.Closer, error) {
	if socket == "" {
		socket = os.Getenv("SSH_AUTH_SOCK")
	}
	if socket == "" {
		return nil, nil, errors.New("ssh: agent socket required")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, nil, err
	}
	return gossh.PublicKeysCallback(agent.NewClient(conn).Signers), conn, nil
}

type Client struct {
	conn *gossh.Client
	cfg  SSHConfig
	Host string
}

func NewSSHClient(ctx context.Context, cfg SSHConfig) (*Client, error) {
	if ctx == nil || cfg.User == "" || len(cfg.Hosts) == 0 || cfg.Timeout < 0 || cfg.MaxOutputBytes < 0 || cfg.MaxFileBytes < 0 {
		return nil, errors.New("ssh: invalid configuration")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.MaxOutputBytes == 0 {
		cfg.MaxOutputBytes = 4 << 20
	}
	if cfg.MaxFileBytes == 0 {
		cfg.MaxFileBytes = 10 << 30
	}
	hostKey := cfg.HostKeyCallback
	if hostKey == nil {
		if cfg.KnownHostsFile == "" {
			return nil, errors.New("ssh: host key verification is required")
		}
		var err error
		hostKey, err = knownhosts.New(cfg.KnownHostsFile)
		if err != nil {
			return nil, err
		}
	}
	var failures []error
	for _, host := range cfg.Hosts {
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, "22")
		}
		bounded, cancel := context.WithTimeout(ctx, cfg.Timeout)
		raw, err := (&net.Dialer{}).DialContext(bounded, "tcp", host)
		if err == nil {
			if deadline, ok := bounded.Deadline(); ok {
				raw.SetDeadline(deadline)
			}
			stop := context.AfterFunc(bounded, func() { raw.Close() })
			var connection gossh.Conn
			var channels <-chan gossh.NewChannel
			var requests <-chan *gossh.Request
			connection, channels, requests, err = gossh.NewClientConn(raw, host, &gossh.ClientConfig{User: cfg.User, Auth: cfg.Auth, HostKeyCallback: hostKey})
			if err == nil && bounded.Err() == nil && stop() {
				raw.SetDeadline(time.Time{})
				cancel()
				return &Client{conn: gossh.NewClient(connection, channels, requests), cfg: cfg, Host: host}, nil
			}
			stop()
			raw.Close()
		}
		if err == nil {
			err = bounded.Err()
		}
		cancel()
		failures = append(failures, fmt.Errorf("ssh: connect %s: %w", host, err))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.Join(failures...)
}
func (c *Client) Close() error { return c.conn.Close() }

type CommandResult struct {
	Stdout, Stderr string
	ExitCode       int
}
type boundedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.buf.Write(p)
	return n, nil
}
func (c *Client) ExecuteCommand(ctx context.Context, command string) (CommandResult, error) {
	return c.execute(ctx, command, nil)
}

// ExecuteCommandWithSudo passes the password on stdin, never in command arguments.
// The remote command is quoted as one sh -c argument; only trusted commands belong here.
func (c *Client) ExecuteCommandWithSudo(ctx context.Context, command, password string) (CommandResult, error) {
	if strings.ContainsAny(password, "\r\n") {
		return CommandResult{}, errors.New("ssh: invalid sudo password")
	}
	return c.execute(ctx, "sudo -S -p '' -- sh -c "+shellQuote(command), strings.NewReader(password+"\n"))
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (c *Client) execute(ctx context.Context, command string, input io.Reader) (CommandResult, error) {
	if ctx == nil || strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) {
		return CommandResult{}, errors.New("ssh: context/command required")
	}
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	stopConnection := context.AfterFunc(bounded, func() { c.conn.Close() })
	session, err := c.conn.NewSession()
	if err != nil {
		stopConnection()
		return CommandResult{}, err
	}
	if !stopConnection() {
		session.Close()
		return CommandResult{}, bounded.Err()
	}
	defer session.Close()
	stop := context.AfterFunc(bounded, func() { session.Close() })
	defer stop()
	out, stderr := &boundedBuffer{limit: c.cfg.MaxOutputBytes}, &boundedBuffer{limit: c.cfg.MaxOutputBytes}
	session.Stdout = out
	session.Stderr = stderr
	session.Stdin = input
	err = session.Run(command)
	result := CommandResult{Stdout: out.buf.String(), Stderr: stderr.buf.String()}
	if err != nil {
		result.ExitCode = -1
		var exit *gossh.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitStatus()
		}
	}
	if bounded.Err() != nil {
		return result, bounded.Err()
	}
	if out.overflow || stderr.overflow {
		return result, errors.New("ssh: command output exceeds limit")
	}
	return result, err
}

type Pool struct {
	mu      sync.Mutex
	cfg     SSHConfig
	slots   chan struct{}
	idle    []*Client
	closed  bool
	closing chan struct{}
}

func NewPool(cfg SSHConfig, size int) (*Pool, error) {
	if size < 1 {
		return nil, errors.New("ssh: positive pool size required")
	}
	return &Pool{cfg: cfg, slots: make(chan struct{}, size), closing: make(chan struct{})}, nil
}

// Acquire returns a once-only release function. Mark broken=true on transport
// failure; command exit status alone does not make a connection broken.
func (p *Pool) Acquire(ctx context.Context) (*Client, func(bool), error) {
	if ctx == nil {
		return nil, nil, errors.New("ssh: context required")
	}
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-p.closing:
		return nil, nil, os.ErrClosed
	case p.slots <- struct{}{}:
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.slots
		return nil, nil, os.ErrClosed
	}
	var c *Client
	if n := len(p.idle); n > 0 {
		c = p.idle[n-1]
		p.idle = p.idle[:n-1]
	}
	p.mu.Unlock()
	var err error
	if c == nil {
		c, err = NewSSHClient(ctx, p.cfg)
	}
	if err != nil {
		<-p.slots
		return nil, nil, err
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		c.Close()
		<-p.slots
		return nil, nil, os.ErrClosed
	}
	var once sync.Once
	release := func(broken bool) {
		once.Do(func() {
			p.mu.Lock()
			if broken || p.closed {
				c.Close()
			} else {
				p.idle = append(p.idle, c)
			}
			p.mu.Unlock()
			<-p.slots
		})
	}
	return c, release, nil
}
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	close(p.closing)
	var errs []error
	for _, c := range p.idle {
		errs = append(errs, c.Close())
	}
	p.idle = nil
	return errors.Join(errs...)
}
