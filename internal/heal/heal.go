// Package heal recovers a patch after a Claude Code update drifts it past
// structural derivation, by asking Claude itself to re-locate the sites.
package heal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/patcher"
	"github.com/yasyf/cc-patch/internal/registry"
	"github.com/yasyf/cc-patch/internal/store"
)

// Result reports one heal attempt.
type Result struct {
	patcher.Outcome
	// Rederived is true when Claude re-located the sites after derivation failed.
	Rederived bool
}

// Heal applies a patch, escalating to a Claude re-derivation only when both the
// pinned literals and structural derivation fail. Re-derived sites are persisted
// per version so future apply runs reuse them.
func Heal(ctx context.Context, inst claude.Install, p registry.Patch) (Result, error) {
	out, err := patcher.Apply(ctx, inst, p)
	if err == nil {
		return Result{Outcome: out}, nil
	}
	if !errors.Is(err, patcher.ErrDrift) {
		return Result{}, err
	}
	if p.HealPrompt == "" {
		return Result{}, err
	}
	sites, rerr := rederive(ctx, inst, p)
	if rerr != nil {
		return Result{}, fmt.Errorf("heal %s: derivation failed and Claude re-derivation failed: %w", p.ID, rerr)
	}
	if err := validate(inst, p, sites); err != nil {
		return Result{}, fmt.Errorf("heal %s: Claude-derived sites rejected: %w", p.ID, err)
	}
	if err := patcher.PersistSites(inst.Version, p.ID, sites); err != nil {
		return Result{}, err
	}
	out, err = patcher.Apply(ctx, inst, p)
	if err != nil {
		return Result{}, fmt.Errorf("heal %s: apply after re-derivation: %w", p.ID, err)
	}
	return Result{Outcome: out, Rederived: true}, nil
}

type rederivedSite struct {
	Anchor string `json:"anchor"`
	Find   []byte `json:"find"`
	Drop   []byte `json:"drop"`
}

func rederive(ctx context.Context, inst claude.Install, p registry.Patch) ([]registry.Site, error) {
	window, err := binpatch.Window(inst.Binary, p.SegmentName)
	if err != nil {
		return nil, err
	}
	dir, err := store.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	bundle := filepath.Join(dir, "bundle-"+inst.Version+".js")
	if err := os.WriteFile(bundle, window, 0o600); err != nil {
		return nil, fmt.Errorf("write bundle for re-derivation: %w", err)
	}
	defer func() { _ = os.Remove(bundle) }()

	text, err := runClaude(ctx, inst.Launcher, prompt(bundle, p))
	if err != nil {
		return nil, err
	}
	raw, err := extractJSON(text)
	if err != nil {
		return nil, err
	}
	var derived []rederivedSite
	if err := json.Unmarshal(raw, &derived); err != nil {
		return nil, fmt.Errorf("parse Claude sites: %w", err)
	}
	if len(derived) == 0 {
		return nil, errors.New("re-derivation returned no sites")
	}
	sites := make([]registry.Site, len(derived))
	for i, d := range derived {
		sites[i] = registry.Site{Anchor: d.Anchor, Find: d.Find, Drop: d.Drop}
	}
	return sites, nil
}

// prompt renders the pack author's HealPrompt (the task description) framed by the
// engine-owned base64 output contract that extractJSON/rederivedSite depend on.
func prompt(bundlePath string, p registry.Patch) string {
	return fmt.Sprintf(`The file %s is a minified JavaScript bundle from Claude Code.

%s

For each site produce:
- find: the minimal byte substring, unique in the file, that spans the site and contains the fragment to blank.
- drop: a contiguous substring within find to blank to spaces; blanking it must be length-neutral (find and drop are the same length change of zero).

Use your tools to grep the file and verify each find matches exactly once. Output ONLY a JSON array (no prose), where find and drop are base64-encoded raw bytes, one object per site:
[{"anchor":"<label>","find":"<base64>","drop":"<base64>"}]

Patch id: %s.`, bundlePath, p.HealPrompt, p.ID)
}

func runClaude(ctx context.Context, launcher, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, launcher,
		"-p",
		"--model", "opus",
		"--settings", `{"fastMode":true}`,
		"--dangerously-skip-permissions",
		"--output-format", "json",
		prompt,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run claude -p: %w", err)
	}
	var env struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return "", fmt.Errorf("parse claude -p envelope: %w", err)
	}
	return env.Result, nil
}

// extractJSON pulls the JSON array out of Claude's final text, tolerating a
// fenced code block or surrounding whitespace.
func extractJSON(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "["); i >= 0 {
		if j := strings.LastIndex(text, "]"); j > i {
			return []byte(text[i : j+1]), nil
		}
	}
	return nil, fmt.Errorf("no JSON array in Claude output: %q", truncate(text, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func validate(inst claude.Install, p registry.Patch, sites []registry.Site) error {
	// Claude-derived sites are untrusted: reject any drop that is not within its
	// find before Substitutions calls blank(), which panics on a bad pair.
	for i, s := range sites {
		if !bytes.Contains(s.Find, s.Drop) {
			return fmt.Errorf("site %d: drop %q is not within find %q", i, s.Drop, s.Find)
		}
	}
	res, err := binpatch.Status(inst.Binary, p.SegmentName, registry.Substitutions(sites))
	if err != nil {
		return err
	}
	for _, s := range res.Sites {
		if s.State == binpatch.StateMissing {
			return fmt.Errorf("site %d not present in binary", s.Index)
		}
	}
	return nil
}
