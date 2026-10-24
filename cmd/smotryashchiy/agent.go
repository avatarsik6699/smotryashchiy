package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/avatarsik6699/smotryashchiy/internal/agent"
	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

const agentUsage = "usage: smotryashchiy agent enroll --server URL --secret S [--config PATH] | agent run [--config PATH] [--interval 10s] [--spool-dir DIR] [--document KIND=PATH]... [--business-file PATH] [--operator-command PATH] | agent push-file FILE [--config PATH] [--key KEY]"

const defaultAgentConfig = "agent.json"

func runAgent(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(agentUsage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "enroll":
		fs := flag.NewFlagSet("agent enroll", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		server := fs.String("server", "", "server URL")
		secret := fs.String("secret", "", "one-time enrollment secret")
		path := fs.String("config", defaultAgentConfig, "agent config path")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *server == "" || *secret == "" {
			return errors.New(agentUsage)
		}
		cfg, err := agent.Enroll(ctx, *server, *secret, *path)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "enrolled as host %s (tunnel address %s); config written to %s\n", cfg.HostID, cfg.TunnelIP, *path)
		return nil
	case "run":
		return runAgentLoop(ctx, args[1:])
	case "push-file":
		// Flags may follow the file operand, so split it off before parsing.
		if len(args) < 2 {
			return errors.New(agentUsage)
		}
		file := args[1]
		fs := flag.NewFlagSet("agent push-file", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("config", defaultAgentConfig, "agent config path")
		key := fs.String("key", "", "Idempotency-Key (default: random)")
		if err := fs.Parse(args[2:]); err != nil || fs.NArg() != 0 {
			return errors.New(agentUsage)
		}
		cfg, err := agent.LoadConfig(*path)
		if err != nil {
			return err
		}
		batch, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read batch file: %w", err)
		}
		res, err := agent.Push(ctx, cfg, batch, *key)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "accepted metrics=%d checks=%d events=%d; duplicates metrics=%d checks=%d events=%d; replayed=%t\n",
			res.Accepted.Metrics, res.Accepted.Checks, res.Accepted.Events,
			res.Duplicates.Metrics, res.Duplicates.Checks, res.Duplicates.Events, res.Replayed)
		return nil
	default:
		return errors.New(agentUsage)
	}
}

// runAgentLoop is `agent run`: collect on an interval and deliver through one persistent tunnel.
func runAgentLoop(ctx context.Context, args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("agent run: host metrics are only supported on Linux, this is %s", runtime.GOOS)
	}
	fs := flag.NewFlagSet("agent run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", defaultAgentConfig, "agent config path")
	interval := fs.Duration("interval", agent.DefaultInterval, "collection interval (5s..5m)")
	spoolDir := fs.String("spool-dir", "", "offline buffer directory (default: <config dir>/spool)")
	businessFile := fs.String("business-file", "", "alias of --document business=PATH (Change 27)")
	var documentFlags repeatedFlag
	fs.Var(&documentFlags, "document", "KIND=PATH: report a JSON file as a document of that kind when it changes; repeatable (Change 29)")
	operatorCommand := fs.String("operator-command", "", "the host's operator executable; enables the operator command channel (Change 30)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New(agentUsage)
	}
	if err := agent.ValidateInterval(*interval); err != nil {
		return err
	}
	cfg, err := agent.LoadConfig(*path)
	if err != nil {
		return err
	}
	if *spoolDir == "" {
		*spoolDir = filepath.Join(filepath.Dir(*path), "spool")
	}
	documents, err := documentCollectors(documentFlags, *businessFile)
	if err != nil {
		return err
	}
	if *operatorCommand != "" {
		if !filepath.IsAbs(*operatorCommand) {
			return errors.New("agent run: --operator-command must be an absolute path")
		}
		info, err := os.Stat(*operatorCommand)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("agent run: --operator-command %s must be an executable file", *operatorCommand)
		}
	}
	return agent.Run(ctx, agent.RunConfig{
		OperatorCommand:    *operatorCommand,
		DocumentCollectors: documents,
		Interval:           *interval,
		SpoolDir:           *spoolDir,
		Collectors: []collect.Collector{
			&collect.CPU{Proc: collect.DefaultProc},
			collect.CPUCount{Proc: collect.DefaultProc},
			collect.Pressure{Proc: collect.DefaultProc},
			collect.Memory{Proc: collect.DefaultProc},
			collect.NewDisk(),
			collect.Network{Proc: collect.DefaultProc},
			collect.Load{Proc: collect.DefaultProc},
			collect.Uptime{Proc: collect.DefaultProc},
			collect.NewDocker("").WithDeadline(*interval),
		},
		CheckCollectors: []collect.CheckCollector{
			collect.NewFail2banStatus(),
		},
		EventCollectors: []collect.EventCollector{
			collect.NewJournald(ctx),
			collect.NewFail2banEvents(ctx, ""),
			collect.NewDockerLogs(ctx, ""),
		},
		Dial: func(ctx context.Context) (agent.Uplink, error) { return agent.Dial(ctx, cfg) },
		Log:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
}

// repeatedFlag collects every occurrence of a repeatable flag.
type repeatedFlag []string

func (r *repeatedFlag) String() string     { return strings.Join(*r, ",") }
func (r *repeatedFlag) Set(v string) error { *r = append(*r, v); return nil }

// documentCollectors turns --document KIND=PATH flags (and the --business-file alias) into file
// document collectors. Kinds must be valid and unique, and a batch carries at most
// domain.MaxDocuments of them, so a bad flag fails at start rather than in every batch.
func documentCollectors(flags []string, businessFile string) ([]collect.DocumentCollector, error) {
	if businessFile != "" {
		flags = append(flags, "business="+businessFile)
	}
	if len(flags) > domain.MaxDocuments {
		return nil, fmt.Errorf("agent run: at most %d --document flags, got %d", domain.MaxDocuments, len(flags))
	}
	seen := make(map[string]bool, len(flags))
	out := make([]collect.DocumentCollector, 0, len(flags))
	for _, f := range flags {
		kind, path, ok := strings.Cut(f, "=")
		if !ok || path == "" || !domain.ValidDocumentKind(kind) {
			return nil, fmt.Errorf("agent run: --document %q must be KIND=PATH with KIND matching ^[a-z][a-z0-9_.]{0,63}$", f)
		}
		if seen[kind] {
			return nil, fmt.Errorf("agent run: document kind %q is given twice", kind)
		}
		seen[kind] = true
		out = append(out, &collect.FileDocument{Kind: kind, Path: path})
	}
	return out, nil
}
