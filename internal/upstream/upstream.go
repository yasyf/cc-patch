// Package upstream resolves the GitHub issue a patch works around, so a patch
// retires itself once the upstream bug is fixed.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yasyf/cc-patch/internal/store"
)

// TTL is how long a resolved issue state stays cached, matching the daily heal
// cadence.
const TTL = 24 * time.Hour

var (
	refPattern = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)#([0-9]+)$`)
	apiBase    = "https://api.github.com"
)

// Ref is a GitHub issue reference of the form "owner/repo#number".
type Ref struct {
	Owner  string
	Repo   string
	Number int
}

// Parse decodes an "owner/repo#number" reference.
func Parse(s string) (Ref, error) {
	m := refPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Ref{}, fmt.Errorf("upstream issue %q must look like owner/repo#123", s)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return Ref{}, fmt.Errorf("upstream issue %q: %w", s, err)
	}
	if n == 0 {
		return Ref{}, fmt.Errorf("upstream issue %q: number must be positive", s)
	}
	return Ref{Owner: m[1], Repo: m[2], Number: n}, nil
}

// Zero reports whether the ref is unset — a patch with no upstream link.
func (r Ref) Zero() bool { return r.Number == 0 }

// String renders the ref as "owner/repo#number".
func (r Ref) String() string {
	if r.Zero() {
		return ""
	}
	return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number)
}

// URL is the issue's address on github.com.
func (r Ref) URL() string {
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", r.Owner, r.Repo, r.Number)
}

func (r Ref) apiURL() string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d", apiBase, r.Owner, r.Repo, r.Number)
}

// Fetch reports whether the referenced issue is closed, querying GitHub
// unauthenticated. A pull request that merged counts as closed.
func Fetch(ctx context.Context, r Ref) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.apiURL(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("query %s: %w", r, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("query %s: github returned %s", r, resp.Status)
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, fmt.Errorf("query %s: %w", r, err)
	}
	return body.State == "closed", nil
}

type cacheEntry struct {
	Closed    bool      `json:"closed"`
	CheckedAt time.Time `json:"checked_at"`
}

// cachePath keeps resolved issue states beside the state document rather than
// inside it: state.json is fingerprint-validated, and widening its schema would
// reject every state file written by an older cc-patch.
func cachePath() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "upstream.json"), nil
}

func readCache() (map[string]cacheEntry, error) {
	p, err := cachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]cacheEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read upstream cache %q: %w", p, err)
	}
	var m map[string]cacheEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse upstream cache %q: %w", p, err)
	}
	if m == nil {
		m = map[string]cacheEntry{}
	}
	return m, nil
}

func writeCache(m map[string]cacheEntry) error {
	p, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write upstream cache %q: %w", p, err)
	}
	return nil
}

// Retired reports whether a patch's upstream issue has closed, reading the cache
// before querying GitHub. A failed lookup reports not-retired with the error, so
// a patch that cannot be checked keeps applying.
func Retired(ctx context.Context, r Ref) (bool, error) {
	if r.Zero() {
		return false, nil
	}
	cache, err := readCache()
	if err != nil {
		return false, err
	}
	now := time.Now()
	if entry, ok := cache[r.String()]; ok && now.Sub(entry.CheckedAt) < TTL {
		return entry.Closed, nil
	}
	closed, err := Fetch(ctx, r)
	if err != nil {
		return false, err
	}
	cache[r.String()] = cacheEntry{Closed: closed, CheckedAt: now}
	if err := writeCache(cache); err != nil {
		return false, err
	}
	return closed, nil
}
