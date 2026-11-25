package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// LocalHost is the bench name for the machine the launcher runs on: its
// shards are started with bash, not ssh. A scheduled workflow on a schema
// runner names it.
const LocalHost = "local"

// fleetRemote reaches LocalHost with bash and every other bench with ssh.
type fleetRemote struct{ ssh sshRemote }

func (f fleetRemote) Run(ctx context.Context, host, script string, stdin []byte) ([]byte, error) {
	if host != LocalHost {
		return f.ssh.Run(ctx, host, script, stdin)
	}
	c := exec.CommandContext(ctx, "bash", "-c", script)
	c.Dir = os.Getenv("HOME")
	c.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("local: %w: %s", err, strings.TrimSpace(tailN(errb.String(), 8)))
	}
	return out.Bytes(), nil
}

func (f fleetRemote) Fetch(ctx context.Context, host, dir, local string) error {
	if host != LocalHost {
		return f.ssh.Fetch(ctx, host, dir, local)
	}
	c := exec.CommandContext(ctx, "bash", "-c", `tar -C `+hp(dir)+` -cf - .`)
	pipe, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	c.Stderr = &errb
	if err := c.Start(); err != nil {
		return err
	}
	untarErr := untar(pipe, local)
	_, _ = io.Copy(io.Discard, pipe) // tar is not left blocked on a full pipe
	if err := c.Wait(); err != nil {
		return fmt.Errorf("fetch local %s: %w: %s", dir, err, strings.TrimSpace(errb.String()))
	}
	return untarErr
}
