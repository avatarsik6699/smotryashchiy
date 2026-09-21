// Command smotryashchiy is the single binary of the monitoring system: "server", "agent" and
// "admin" modes (docs/SPEC.md §3).
package main

import (
	"fmt"
	"os"
)

// release is the build-time release SHA, set with -ldflags "-X main.release=<sha>".
var release = "development"

const usage = "usage: smotryashchiy server | agent enroll|push-file | admin set-password|host create"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin *os.File, stdout, stderr *os.File) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "server":
		err = runServer(stdout)
	case "agent":
		err = runAgent(args[1:], stdout)
	case "admin":
		err = runAdmin(args[1:], stdin, stdout)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
