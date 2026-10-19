package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type textFuzzReport struct {
	Unknown      int32 `json:"unknown"`
	KindMismatch int32 `json:"kind_mismatch"`
	Widened      int32 `json:"widened"`
	Clamped      int32 `json:"clamped"`
	Duplicate    int32 `json:"duplicate"`
	Malformed    bool  `json:"malformed"`
	Refused      bool  `json:"refused"`
}

type textFuzzReply struct {
	Loaded bool           `json:"loaded"`
	Report textFuzzReport `json:"report"`
	Wire   []byte         `json:"wire,omitempty"`
}

type textFuzzOptions struct {
	driver string
	unit   string
	root   string
	replay string
}

// textFuzz compares a generated reader's direct JSON entry point to the
// compiler-side engine on exactly one arbitrary payload. A refusal is compared
// by its typed report; accepted values must re-encode to the same wire bytes.
func textFuzz(opts textFuzzOptions) error {
	if opts.driver == "" || opts.unit == "" || opts.root == "" || opts.replay == "" {
		return fmt.Errorf("text-fuzz needs --driver, --unit, --root and --replay")
	}
	input, err := os.ReadFile(opts.replay)
	if err != nil {
		return err
	}
	manifest, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		return err
	}
	unit, err := newUnits(manifest).get(opts.unit)
	if err != nil {
		return err
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup(opts.root)
	if def == nil {
		return fmt.Errorf("unit %s has no table %s", opts.unit, opts.root)
	}
	inst := model.New(def)
	var report tabletext.Report
	loaded := model.Read(inst, input, &report)
	want := textFuzzReply{Loaded: loaded, Report: textFuzzReport{int32(report.Unknown), int32(report.KindMismatch), int32(report.Widened), int32(report.Clamped), int32(report.Duplicate), report.Malformed, report.Refused}}
	if loaded {
		want.Wire, err = tablewire.Encode(model, inst)
		if err != nil {
			return fmt.Errorf("engine encoded accepted text: %w", err)
		}
	}
	parts := strings.Fields(opts.driver)
	if len(parts) == 0 {
		return fmt.Errorf("text-fuzz has an empty driver command")
	}
	if len(parts) != 1 {
		return fmt.Errorf("text-fuzz --driver expects the generated reader executable only")
	}
	args := []string{"reader-fuzz", opts.unit, opts.root, "json"}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, parts[0], args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("generated reader timed out for %s.%s: %s", opts.unit, opts.root, stderr.String())
		}
		return fmt.Errorf("generated reader failed for %s.%s: %w: %s", opts.unit, opts.root, err, stderr.String())
	}
	var got textFuzzReply
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		return fmt.Errorf("generated reader returned invalid protocol data: %w: %s", err, stdout.String())
	}
	if got.Loaded != want.Loaded || got.Report != want.Report || got.Loaded && !bytes.Equal(got.Wire, want.Wire) {
		return fmt.Errorf("differential reader mismatch %s.%s: got loaded=%v report=%+v wire=%x; engine loaded=%v report=%+v wire=%x", opts.unit, opts.root, got.Loaded, got.Report, got.Wire, want.Loaded, want.Report, want.Wire)
	}
	return nil
}
