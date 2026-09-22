package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	authapp "github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
	telemetryapp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	telemetryinfra "github.com/avatarsik6699/smotryashchiy/internal/telemetry/infrastructure"
)

// maxPasswordInput bounds stdin reads (1024-byte limit plus a trailing newline and one probe byte).
const maxPasswordInput = 1026

const adminUsage = "usage: smotryashchiy admin set-password (password read from stdin) | admin host create --name NAME [--server URL] | admin backup --out FILE|- | admin restore --from FILE|- [--force]"

func runAdmin(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) >= 2 && args[0] == "host" && args[1] == "create" {
		return runHostCreate(args[2:], stdout)
	}
	if len(args) >= 1 && args[0] == "backup" {
		return runBackup(args[1:], stdout)
	}
	if len(args) >= 1 && args[0] == "restore" {
		return runRestore(args[1:], stdin, stdout)
	}
	if len(args) != 1 || args[0] != "set-password" {
		return errors.New(adminUsage)
	}
	cfg, err := config.Load(release)
	if err != nil {
		return err
	}
	password, err := readPassword(stdin)
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := db.Migrate(sqlDB); err != nil {
		return err
	}
	svc := authapp.NewService(authinfra.NewSettingsStore(sqlDB))
	if err := svc.SetAdminPassword(context.Background(), password); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "admin password initialized")
	return nil
}

// readPassword reads one password line from r; it never touches argv or the environment.
func readPassword(r io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxPasswordInput))
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	if len(raw) >= maxPasswordInput {
		return "", errors.New("password exceeds safety limit")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"), nil
}

// runHostCreate registers a host and prints its one-time enrollment command. The secret is shown
// once and never stored; only its hash is.
func runHostCreate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("admin host create", flag.ContinueOnError)
	name := fs.String("name", "", "host name (1..80 bytes)")
	server := fs.String("server", "", "server URL agents use for enrollment (default http://127.0.0.1:<port of SMOTRYASHCHIY_ADDR>)")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *name == "" {
		return errors.New(adminUsage)
	}
	cfg, err := config.Load(release)
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := db.Migrate(sqlDB); err != nil {
		return err
	}
	svc := telemetryapp.NewEnrollmentService(telemetryinfra.NewStore(sqlDB), nil, cfg.TunnelCIDR, telemetryapp.ServerInfo{}, time.Now)
	host, secret, expires, err := svc.CreateHost(context.Background(), *name)
	if err != nil {
		return err
	}
	url := *server
	if url == "" {
		url = defaultServerURL(cfg.Addr)
	}
	fmt.Fprintf(stdout, "host %q created (id %s). The secret is shown once and expires at %s:\n\n  smotryashchiy agent enroll --server %s --secret %s\n",
		host.Name, host.ID, expires.UTC().Format(time.RFC3339), url, secret)
	return nil
}

// defaultServerURL turns a listen address like ":8080" into a URL reachable from the same machine.
func defaultServerURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
