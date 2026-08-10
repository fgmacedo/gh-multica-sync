// Package ghcli shells out to gh.
//
// Authentication is delegated to gh: anyone installing this as a gh extension
// already has gh authenticated, so there is no new token to create, store or
// rotate.
package ghcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner exists so commands can be tested without an external process.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type Exec struct{ Bin string }

func New() *Exec { return &Exec{Bin: "gh"} }

func (e *Exec) Run(ctx context.Context, args ...string) ([]byte, error) {
	bin := e.Bin
	if bin == "" {
		bin = "gh"
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// Authenticated reports whether gh can talk to GitHub right now.
func Authenticated(ctx context.Context, r Runner) error {
	_, err := r.Run(ctx, "auth", "status")
	return err
}
