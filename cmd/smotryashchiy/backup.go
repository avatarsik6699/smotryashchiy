package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/backup"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

// runBackup writes a consistent snapshot bundle of the (possibly running) server's database.
func runBackup(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("admin backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "bundle file to create (.tar.gz, must not exist), or - for stdout")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *out == "" {
		return errors.New(adminUsage)
	}
	cfg, err := config.Load(release)
	if err != nil {
		return err
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return fmt.Errorf("no database at %s: %w", cfg.DBPath, err)
	}
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	workDir := filepath.Dir(cfg.DBPath) // writable even when the root filesystem is read-only
	if *out == "-" {
		// Streaming to stdout: the bundle is the only thing written there; notes go to stderr.
		if isTerminal(os.Stdout) {
			return errors.New("refusing to write a binary bundle to a terminal; redirect stdout to a file (umask 077) or use --out FILE")
		}
		m, err := backup.Create(context.Background(), sqlDB, release, workDir, os.Stdout, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "backup streamed (%d bytes of database, %d migrations, sha256 %s)\nthe bundle contains secrets (admin password hash, WireGuard key): keep it private\n", m.DBBytes, m.Migrations, m.DBSHA256)
		return nil
	}
	m, err := backup.CreateFile(context.Background(), sqlDB, release, workDir, *out, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "backup written to %s (%d bytes of database, %d migrations, sha256 %s)\n", *out, m.DBBytes, m.Migrations, m.DBSHA256)
	fmt.Fprintln(stdout, "the bundle contains secrets (admin password hash, WireGuard key): keep it private")
	return nil
}

// isTerminal reports whether f is an interactive terminal (a character device).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// runRestore replaces the database with the one in a bundle. The server must be stopped.
func runRestore(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("admin restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	from := fs.String("from", "", "bundle file to restore, or - for stdin")
	force := fs.Bool("force", false, "replace an existing non-empty database (it is kept aside)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *from == "" {
		return errors.New(adminUsage)
	}
	cfg, err := config.Load(release)
	if err != nil {
		return err
	}
	bundle := stdin
	if *from != "-" {
		f, err := os.Open(*from)
		if err != nil {
			return fmt.Errorf("open bundle: %w", err)
		}
		defer f.Close()
		bundle = f
	}
	res, err := backup.Restore(context.Background(), bundle, cfg.DBPath, backup.RestoreOptions{Force: *force, MaxMigrations: db.MigrationCount(), Now: time.Now()})
	if err != nil {
		return err
	}
	source := *from
	if source == "-" {
		source = "the bundle from stdin"
	}
	fmt.Fprintf(stdout, "restored %s into %s (taken %s by release %s)\n", source, cfg.DBPath, res.Manifest.CreatedAt.Format(time.RFC3339), res.Manifest.Release)
	if res.PreviousDB != "" {
		fmt.Fprintf(stdout, "the previous database was kept at %s\n", res.PreviousDB)
	}
	fmt.Fprintln(stdout, "start the server again to use it")
	return nil
}
