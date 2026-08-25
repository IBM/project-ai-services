package datasourceservice

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	sshDialTimeout = 10 * time.Second
)

// fileSystemTester implements ConnectionTester for SSH/SFTP-based file system sources.
type fileSystemTester struct{}

// NewFileSystemTester returns a ConnectionTester for SSH/SFTP file system providers.
func NewFileSystemTester() ConnectionTester {
	return &fileSystemTester{}
}

// TestConnection runs three sequential checks against an SSH/SFTP endpoint:
//  1. Network — TCP dial to host (port defaults to 22 when not supplied).
//  2. Auth    — SSH handshake using the PEM private key.
//  3. Access  — SFTP Stat on remote_path.
//
// The overall context deadline is propagated to the raw TCP connection so that
// every subsequent operation (SSH handshake, SFTP subsystem, Stat) is
// automatically cancelled when the deadline expires.
func (t *fileSystemTester) TestConnection(ctx context.Context, params map[string]any) error {
	host, _ := params["host"].(string)
	username, _ := params["username"].(string)
	privateKeyPEM, _ := params["private_key"].(string)
	remotePath, _ := params["remote_path"].(string)

	// Port is not a required schema field; default to 22 when absent.
	port := "22"
	if p, ok := params["port"].(string); ok && p != "" {
		port = p
	}

	// ── 1. Network ────────────────────────────────────────────────────────────
	addr := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: sshDialTimeout}

	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckNetwork,
			Message:   fmt.Sprintf("TCP dial %s failed: %v", addr, err),
		}
	}

	// Apply the remaining context deadline to the raw connection so every
	// subsequent operation (SSH handshake, SFTP I/O) is bounded by it.
	if deadline, ok := ctx.Deadline(); ok {
		_ = tcpConn.SetDeadline(deadline)
	}

	// ── Parse private key ─────────────────────────────────────────────────────
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		_ = tcpConn.Close()

		return &ConnectionCheckError{
			CheckType: ConnectionCheckAuth,
			Message:   "could not parse private_key: ensure it is a valid PEM-encoded key with newlines preserved (\\n between lines)",
		}
	}

	// ── 2. Auth — SSH handshake ───────────────────────────────────────────────
	sshCfg := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 — diagnostic probe; not used on the data path
		Timeout:         sshDialTimeout,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, sshCfg)
	if err != nil {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAuth,
			Message:   "SSH authentication failed: verify the username and private key are correct for the target host",
		}
	}

	sshClient := ssh.NewClient(sshConn, chans, reqs)
	defer func() { _ = sshClient.Close() }()

	// ── 3. Access — SFTP stat ────────────────────────────────────────────────
	return checkSFTPAccess(sshClient, remotePath)
}

// checkSFTPAccess opens an SFTP subsystem and stats the remote path to confirm it
// exists and is reachable. Stat (SSH_FXP_STAT) is sufficient: a successful response
// proves the authenticated user has at least execute permission on the path and that
// the path is accessible — which is the meaningful check for a datasource connection
// test. ReadDir is intentionally avoided here: it buffers every directory entry from
// the server before returning, which is unbounded in time for large directories.
// The underlying TCP connection already has a deadline so all SFTP calls are bounded.
func checkSFTPAccess(sshClient *ssh.Client, remotePath string) error {
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAccess,
			Message:   fmt.Sprintf("could not open SFTP subsystem: %v", err),
		}
	}
	defer func() { _ = sftpClient.Close() }()

	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAccess,
			Message:   fmt.Sprintf("sftp.Stat(%q) failed: %v", remotePath, err),
		}
	}

	if !info.IsDir() {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAccess,
			Message:   fmt.Sprintf("remote path %q is not a directory", remotePath),
		}
	}

	return nil
}

// Made with Bob
