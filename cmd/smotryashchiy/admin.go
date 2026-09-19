package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	authapp "github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

// maxPasswordInput bounds stdin reads (1024-byte limit plus a trailing newline and one probe byte).
const maxPasswordInput = 1026

func runAdmin(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "set-password" {
		return errors.New("usage: smotryashchiy admin set-password (password read from stdin)")
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
