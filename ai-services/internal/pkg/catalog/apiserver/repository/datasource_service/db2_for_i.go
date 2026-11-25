package datasourceservice

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	mapepire "github.com/deady54/mapepire-go"
	"github.com/gorilla/websocket"
)

const (
	// db2ForIDefaultPort is the default Mapepire daemon port.
	db2ForIDefaultPort = "8076"
	// db2ForITimeout bounds each phase (network, auth, access) of the connection test.
	db2ForITimeout = 10 * time.Second
	// db2ForIJobID is the identifier of the short-lived Mapepire SQL job used for the test.
	db2ForIJobID = "db2-for-i-test"
	// db2ForIAccessQuery is a read-only probe that returns the configured database (schema/library)
	// when it exists and the authenticated user is authorized to see it.
	db2ForIAccessQuery = "SELECT SCHEMA_NAME FROM QSYS2.SYSSCHEMAS WHERE SCHEMA_NAME = ?"
	// db2ForIProperties works around a misspelled JSON key in the mapepire-go SDK connect payload: the value injects
	// "technique":"TCP" into the connect payload, forcing the TCP Db2 driver and avoiding the
	// CCSID 65535 exception. It must be set verbatim.
	db2ForIProperties = "translate binary=true;ccsid=37\",\"technique\":\"TCP"
)

// mapepireDialerMu synchronizes custom TLS configuration changes applied to websocket.DefaultDialer.
var mapepireDialerMu sync.Mutex

// db2ForITester implements ConnectionTester for IBM Db2 for i accessed through Mapepire.
type db2ForITester struct{}

// NewDb2ForITester returns a ConnectionTester for IBM Db2 for i providers.
func NewDb2ForITester() ConnectionTester {
	return &db2ForITester{}
}

// TestConnection runs three sequential checks against a Mapepire daemon:
//  1. Network — TCP dial to host:port (port defaults to 8076 when not supplied) followed by a
//     TLS handshake that verifies the server certificate (using the optional certificate
//     or insecure setting if provided).
//  2. Auth    — Mapepire WebSocket handshake and IBM i authentication.
//  3. Access  — read-only query confirming the configured database is accessible to the user.
//
// Certificate verification is performed unless allow_insecure_connection is enabled.
func (t *db2ForITester) TestConnection(ctx context.Context, params map[string]any) error {
	host, _ := params["host"].(string)
	database, _ := params["database"].(string)
	username, _ := params["username"].(string)
	password, _ := params["password"].(string)
	port := db2ForIPort(params["port"])
	certificate, _ := params["certificate"].(string)
	allowInsecure, _ := params["allow_insecure_connection"].(bool)

	tlsCfg, err := db2ForITLSConfig(host, certificate, allowInsecure)
	if err != nil {
		return err
	}

	if err := checkDb2ForINetwork(ctx, host, port, tlsCfg); err != nil {
		return err
	}

	job, err := connectDb2ForIJob(ctx, mapepire.DaemonServer{
		Host:               host,
		User:               username,
		Password:           password,
		Port:               port,
		IgnoreUnauthorized: allowInsecure,
		Properties:         db2ForIProperties,
	}, username, tlsCfg)
	if err != nil {
		return err
	}
	defer func() { _ = job.Close() }()

	return checkDb2ForIAccess(ctx, job, database)
}

// db2ForIPort resolves the port from the stored parameter, which may be a string, a JSON
// number or an integer. It falls back to the Mapepire default when absent or empty.
func db2ForIPort(raw any) string {
	switch v := raw.(type) {
	case string:
		if v != "" {
			return v
		}
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	}

	return db2ForIDefaultPort
}

// db2ForITLSConfig creates a *tls.Config with the given host, optional PEM certificate, and insecure setting.
func db2ForITLSConfig(host, certPEM string, allowInsecure bool) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: allowInsecure,
	}

	if certPEM != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(certPEM)) {
			return nil, &ConnectionCheckError{
				CheckType: ConnectionCheckAuth,
				Message:   "invalid certificate: failed to parse PEM certificate",
			}
		}
		if !allowInsecure {
			tlsCfg.RootCAs = pool
		}
	}

	return tlsCfg, nil
}

// checkDb2ForINetwork verifies TCP reachability of the Mapepire daemon and that its TLS
// certificate can be verified. The Mapepire SDK flattens connect errors into strings, so a
// certificate failure there is indistinguishable from a credential failure; checking it here
// keeps it classified as a network error.
func checkDb2ForINetwork(ctx context.Context, host, port string, tlsCfg *tls.Config) error {
	addr := net.JoinHostPort(host, port)

	dialCtx, cancel := context.WithTimeout(ctx, db2ForITimeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: db2ForITimeout}

	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckNetwork,
			Message:   fmt.Sprintf("TCP dial %s failed: %v", addr, err),
		}
	}
	// The Mapepire SDK opens its own WebSocket connection.
	defer func() { _ = conn.Close() }()

	return checkDb2ForITLS(dialCtx, conn, addr, tlsCfg)
}

// checkDb2ForITLS performs a TLS handshake over an established TCP connection using the provided
// TLS configuration. Only certificate verification failures are reported; other handshake failures
// are left to the connect step.
func checkDb2ForITLS(ctx context.Context, conn net.Conn, addr string, tlsCfg *tls.Config) error {
	err := tls.Client(conn, tlsCfg).HandshakeContext(ctx)

	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckNetwork,
			Message:   fmt.Sprintf("TLS handshake with %s failed: %v", addr, err),
		}
	}

	return nil
}

// connectDb2ForIJob opens an authenticated Mapepire SQL job. Connect does not accept a
// context, so it is raced against the phase timeout; a job that connects late is closed.
func connectDb2ForIJob(ctx context.Context, creds mapepire.DaemonServer, username string, tlsCfg *tls.Config) (*mapepire.SQLJob, error) {
	job := mapepire.NewSQLJob(db2ForIJobID)

	_, err := runDb2ForIStep(ctx, func() (struct{}, error) {
		mapepireDialerMu.Lock()
		origTLS := websocket.DefaultDialer.TLSClientConfig
		if tlsCfg != nil && !creds.IgnoreUnauthorized {
			websocket.DefaultDialer.TLSClientConfig = tlsCfg
		}
		defer func() {
			websocket.DefaultDialer.TLSClientConfig = origTLS
			mapepireDialerMu.Unlock()
		}()

		return struct{}{}, job.Connect(creds)
	}, func(struct{}) { _ = job.Close() })
	if err != nil {
		return nil, &ConnectionCheckError{
			CheckType: ConnectionCheckAuth,
			Message:   fmt.Sprintf("Database authentication failed for user %s: check username and password", username),
		}
	}

	return job, nil
}

// checkDb2ForIAccess confirms the configured database (schema/library) exists and that the
// authenticated user is authorized to access it. IBM i object names are upper case, so the
// name is matched case-insensitively. QSYS2.SYSSCHEMAS only lists schemas the user may see.
func checkDb2ForIAccess(ctx context.Context, job *mapepire.SQLJob, database string) error {
	name := strings.ToUpper(strings.TrimSpace(database))

	res, err := runDb2ForIStep(ctx, func() (*mapepire.ServerResponse, error) {
		q, err := job.QueryWithOptions(db2ForIAccessQuery, mapepire.QueryOptions{
			Rows:       1,
			Parameters: [][]any{{name}},
		})
		if err != nil {
			return nil, err
		}

		return q.Execute()
	}, nil)
	if err == nil && (res == nil || !res.Success) {
		err = errors.New("query reported success = false")
	}
	if err != nil {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAccess,
			Message:   fmt.Sprintf("Database query test failed: %v", err),
		}
	}

	if len(res.Data) == 0 {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAccess,
			Message:   fmt.Sprintf("Database %q is not accessible: it does not exist or the user is not authorized to access it", database),
		}
	}

	return nil
}

// runDb2ForIStep runs fn in a goroutine and waits for it or for the phase timeout. If the
// step finishes after the timeout, cleanup (when non-nil) is invoked with its result.
func runDb2ForIStep[T any](ctx context.Context, fn func() (T, error), cleanup func(T)) (T, error) {
	type outcome struct {
		val T
		err error
	}

	stepCtx, cancel := context.WithTimeout(ctx, db2ForITimeout)
	defer cancel()

	done := make(chan outcome, 1)

	go func() {
		val, err := fn()
		done <- outcome{val: val, err: err}
	}()

	select {
	case o := <-done:
		return o.val, o.err
	case <-stepCtx.Done():
		if cleanup != nil {
			go func() {
				if o := <-done; o.err == nil {
					cleanup(o.val)
				}
			}()
		}

		var zero T

		return zero, fmt.Errorf("timed out after %v: %w", db2ForITimeout, stepCtx.Err())
	}
}
