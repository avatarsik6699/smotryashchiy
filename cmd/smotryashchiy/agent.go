package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/avatarsik6699/smotryashchiy/internal/agent"
)

const agentUsage = "usage: smotryashchiy agent enroll --server URL --secret S [--config PATH] | agent push-file FILE [--config PATH] [--key KEY]"

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
