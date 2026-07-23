// Package store persists cc-patch runtime state — patch sites that heal
// re-derived for a specific Claude Code version — so later apply runs reuse them
// without re-invoking Claude.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

var stateMu sync.Mutex

const (
	stateSchemaIdentity    = "dev.yasyf.cc-patch.state"
	stateSchemaVersion     = 1
	stateSchemaDescriptor  = `payload{overrides:map<string<version/patchID>,array<site{anchor:string,find:bytes<nonempty>,drop:bytes<nonempty-substring-of-find>}><nonempty>>,packs:array<pack<oneof builtin{name:string<component>,builtin:true,owner:"",repo:"",ref:"",commit:""}|remote{name:"",builtin:false,owner:string<component>,repo:string<component>,ref:string,commit:string<nonempty>}>,unique(namespace)>}`
	stateSchemaFingerprint = "dev.yasyf.cc-patch.state.3fe1393c998608169f67bdc07db3e0654dcc8825d173f02844ea09da4edffbaa"
)

// Site is a persisted patch site. []byte fields JSON-encode as base64, so the
// raw minified-bundle bytes round-trip safely.
type Site struct {
	Anchor string `json:"anchor"`
	Find   []byte `json:"find"`
	Drop   []byte `json:"drop"`
}

// State is the on-disk document: heal-derived sites keyed by
// "<version>/<patchID>", plus the installed packs.
type State struct {
	Overrides map[string][]Site `json:"overrides"`
	Packs     []InstalledPack   `json:"packs"`
}

// InstalledPack records a pack cc-patch has installed: a builtin (embedded in the
// binary, identified by Name) or a remote repo cloned under PacksDir.
type InstalledPack struct {
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Ref     string `json:"ref"`
	Commit  string `json:"commit"`
}

type persistedEnvelope struct {
	Schema            *string           `json:"schema"`
	SchemaVersion     *int              `json:"schemaVersion"`
	SchemaFingerprint *string           `json:"schemaFingerprint"`
	Payload           *persistedPayload `json:"payload"`
}

type persistedPayload struct {
	Overrides *map[string][]persistedSite `json:"overrides"`
	Packs     *[]persistedPack            `json:"packs"`
}

type persistedSite struct {
	Anchor *string `json:"anchor"`
	Find   *[]byte `json:"find"`
	Drop   *[]byte `json:"drop"`
}

type persistedPack struct {
	Name    *string `json:"name"`
	Builtin *bool   `json:"builtin"`
	Owner   *string `json:"owner"`
	Repo    *string `json:"repo"`
	Ref     *string `json:"ref"`
	Commit  *string `json:"commit"`
}

// Namespace is the pack's identity: its builtin name, or "<owner>/<repo>".
func (p InstalledPack) Namespace() string {
	if p.Builtin {
		return p.Name
	}
	return p.Owner + "/" + p.Repo
}

func key(version, patchID string) string { return version + "/" + patchID }

// Dir is cc-patch's private state directory (~/.local/share/cc-patch).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "cc-patch"), nil
}

// PacksDir is where installed pack repos live (Dir()/packs).
func PacksDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "packs"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

// Load reads the persisted state, returning an empty state when none exists.
func Load() (State, error) {
	p, err := path()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state %q: %w", p, err)
	}
	return decodeState(data, p)
}

// Override returns the derived sites persisted for a version+patch, if any.
func (s State) Override(version, patchID string) ([]Site, bool) {
	sites, ok := s.Overrides[key(version, patchID)]
	return sites, ok
}

// Put records derived sites for a version+patch and writes the state to disk.
func Put(version, patchID string, sites []Site) error {
	return updateState(func(state *State) {
		state.Overrides[key(version, patchID)] = sites
	})
}

// Packs returns the installed packs.
func Packs() ([]InstalledPack, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	return s.Packs, nil
}

// AddPack records an installed pack, replacing any prior entry with the same
// namespace.
func AddPack(p InstalledPack) error {
	return updateState(func(state *State) {
		for i, existing := range state.Packs {
			if existing.Namespace() == p.Namespace() {
				state.Packs[i] = p
				return
			}
		}
		state.Packs = append(state.Packs, p)
	})
}

// RemovePack atomically drops an installed pack and all of its heal overrides.
func RemovePack(namespace string, patchIDs []string) error {
	return updateState(func(state *State) {
		kept := make([]InstalledPack, 0, len(state.Packs))
		for _, pack := range state.Packs {
			if pack.Namespace() != namespace {
				kept = append(kept, pack)
			}
		}
		state.Packs = kept
		want := make(map[string]bool, len(patchIDs))
		for _, id := range patchIDs {
			want[id] = true
		}
		for overrideKey := range state.Overrides {
			if _, patchID, ok := strings.Cut(overrideKey, "/"); ok && want[patchID] {
				delete(state.Overrides, overrideKey)
			}
		}
	})
}

func save(s State) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir %q: %w", dir, err)
	}
	data, err := encodeState(s)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	p, err := path()
	if err != nil {
		return err
	}
	if err := writeDurable(p, data); err != nil {
		return fmt.Errorf("write state %q: %w", p, err)
	}
	return nil
}

func updateState(mutate func(*State)) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir %q: %w", dir, err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open state lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock state: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	state, err := Load()
	if err != nil {
		return err
	}
	mutate(&state)
	return save(state)
}

func emptyState() State {
	return State{
		Overrides: make(map[string][]Site),
		Packs:     make([]InstalledPack, 0),
	}
}

func decodeState(data []byte, path string) (State, error) {
	if err := rejectDuplicateObjectKeys(data); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	var envelope persistedEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return State{}, fmt.Errorf("parse state %q: trailing JSON value", path)
		}
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	if envelope.Schema == nil || *envelope.Schema != stateSchemaIdentity {
		return State{}, fmt.Errorf("state %q: schema must equal %q", path, stateSchemaIdentity)
	}
	if envelope.SchemaVersion == nil || *envelope.SchemaVersion != stateSchemaVersion {
		return State{}, fmt.Errorf("state %q: schemaVersion must equal %d", path, stateSchemaVersion)
	}
	if envelope.SchemaFingerprint == nil || *envelope.SchemaFingerprint != stateSchemaFingerprint {
		return State{}, fmt.Errorf("state %q: schemaFingerprint must equal %q", path, stateSchemaFingerprint)
	}
	if envelope.Payload == nil || envelope.Payload.Overrides == nil || envelope.Payload.Packs == nil {
		return State{}, fmt.Errorf("state %q: payload, overrides, and packs are required", path)
	}

	state := State{
		Overrides: make(map[string][]Site, len(*envelope.Payload.Overrides)),
		Packs:     make([]InstalledPack, len(*envelope.Payload.Packs)),
	}
	for key, persistedSites := range *envelope.Payload.Overrides {
		if persistedSites == nil {
			return State{}, fmt.Errorf("state %q: override %q must be an array", path, key)
		}
		sites := make([]Site, len(persistedSites))
		for i, persisted := range persistedSites {
			if persisted.Anchor == nil || persisted.Find == nil || persisted.Drop == nil {
				return State{}, fmt.Errorf("state %q: override %q site %d requires anchor, find, and drop", path, key, i)
			}
			sites[i] = Site{Anchor: *persisted.Anchor, Find: *persisted.Find, Drop: *persisted.Drop}
		}
		state.Overrides[key] = sites
	}
	for i, persisted := range *envelope.Payload.Packs {
		if persisted.Name == nil || persisted.Builtin == nil || persisted.Owner == nil ||
			persisted.Repo == nil || persisted.Ref == nil || persisted.Commit == nil {
			return State{}, fmt.Errorf("state %q: pack %d requires name, builtin, owner, repo, ref, and commit", path, i)
		}
		state.Packs[i] = InstalledPack{
			Name:    *persisted.Name,
			Builtin: *persisted.Builtin,
			Owner:   *persisted.Owner,
			Repo:    *persisted.Repo,
			Ref:     *persisted.Ref,
			Commit:  *persisted.Commit,
		}
	}
	if err := validateState(state); err != nil {
		return State{}, fmt.Errorf("state %q: %w", path, err)
	}
	return state, nil
}

func encodeState(state State) ([]byte, error) {
	if err := validateState(state); err != nil {
		return nil, err
	}
	envelope := struct {
		Schema            string `json:"schema"`
		SchemaVersion     int    `json:"schemaVersion"`
		SchemaFingerprint string `json:"schemaFingerprint"`
		Payload           State  `json:"payload"`
	}{
		Schema:            stateSchemaIdentity,
		SchemaVersion:     stateSchemaVersion,
		SchemaFingerprint: stateSchemaFingerprint,
		Payload:           state,
	}
	return json.Marshal(envelope)
}

func validateState(state State) error {
	if state.Overrides == nil || state.Packs == nil {
		return errors.New("overrides and packs must be non-nil")
	}
	for overrideKey, sites := range state.Overrides {
		version, patchID, ok := strings.Cut(overrideKey, "/")
		if !ok || version == "" || patchID == "" || strings.Contains(version, "/") {
			return fmt.Errorf("override key %q must be <version>/<patchID>", overrideKey)
		}
		if len(sites) == 0 {
			return fmt.Errorf("override %q must contain at least one site", overrideKey)
		}
		for i, site := range sites {
			if site.Find == nil || site.Drop == nil {
				return fmt.Errorf("override %q site %d find and drop must be non-nil", overrideKey, i)
			}
			if len(site.Find) == 0 || len(site.Drop) == 0 || !bytes.Contains(site.Find, site.Drop) {
				return fmt.Errorf("override %q site %d drop must be a non-empty substring of find", overrideKey, i)
			}
		}
	}
	seen := make(map[string]bool, len(state.Packs))
	for i, pack := range state.Packs {
		switch {
		case pack.Builtin:
			if !validPackComponent(pack.Name) || pack.Owner != "" || pack.Repo != "" || pack.Ref != "" || pack.Commit != "" {
				return fmt.Errorf("pack %d must be exactly one valid builtin identity", i)
			}
		default:
			if pack.Name != "" || !validPackComponent(pack.Owner) || !validPackComponent(pack.Repo) || pack.Commit == "" {
				return fmt.Errorf("pack %d must be exactly one valid remote identity with a commit", i)
			}
		}
		namespace := pack.Namespace()
		if seen[namespace] {
			return fmt.Errorf("pack namespace %q is duplicated", namespace)
		}
		seen[namespace] = true
	}
	return nil
}

func validPackComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`)
}

func rejectDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	return scanJSONValue(decoder)
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = true
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	want := json.Delim('}')
	if delimiter == '[' {
		want = ']'
	}
	if closing != want {
		return fmt.Errorf("unexpected JSON delimiter %q", closing)
	}
	return nil
}

func writeDurable(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporary := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporary)
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod temporary state: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish state: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync state directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close state directory: %w", err)
	}
	return nil
}
