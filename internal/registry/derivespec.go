package registry

import (
	"bytes"
	"fmt"
	"regexp"
)

// GroupRef selects a regex capture group, either by name or by numeric index
// (0 = the whole match). A named ref has Name set; otherwise Index is used.
type GroupRef struct {
	Name  string
	Index int
}

// GroupByName selects a capture group by (?P<name>…).
func GroupByName(name string) GroupRef { return GroupRef{Name: name} }

// GroupByIndex selects a capture group by number (0 = whole match).
func GroupByIndex(index int) GroupRef { return GroupRef{Index: index} }

func (g GroupRef) String() string {
	if g.Name != "" {
		return fmt.Sprintf("%q", g.Name)
	}
	return fmt.Sprintf("#%d", g.Index)
}

func (g GroupRef) index(re *regexp.Regexp) (int, error) {
	if g.Name == "" {
		if g.Index < 0 || g.Index > re.NumSubexp() {
			return 0, fmt.Errorf("group %s out of range (pattern has %d groups)", g, re.NumSubexp())
		}
		return g.Index, nil
	}
	idx := re.SubexpIndex(g.Name)
	if idx < 0 {
		return 0, fmt.Errorf("group %s not in pattern", g)
	}
	return idx, nil
}

func (g GroupRef) resolve(re *regexp.Regexp, window []byte, m []int) ([]byte, error) {
	idx, err := g.index(re)
	if err != nil {
		return nil, err
	}
	start, end := m[2*idx], m[2*idx+1]
	if start < 0 {
		return nil, fmt.Errorf("group %s did not match", g)
	}
	return window[start:end], nil
}

// DeriveSiteSpec is one site of the declarative derive DSL: a pattern matching
// once in the window, Find selecting the bytes to locate, and exactly one of Drop
// (a group within Find to blank) or Replace (a {{group}} template rendering Find's
// substitute). Bind exports captures for a later pattern's {{name}}.
type DeriveSiteSpec struct {
	Anchor     string
	PatternSrc string
	Find       GroupRef
	Drop       *GroupRef
	Replace    string
	Bind       []string
}

// DeriveSpec is the ordered set of derive sites for one patch. Sites evaluate in
// declared order so a later pattern can interpolate an earlier site's binding.
type DeriveSpec struct {
	Sites []DeriveSiteSpec
}

var interpToken = regexp.MustCompile(`\{\{(\w+)\}\}`)

func interpolate(src string, bound map[string][]byte) (string, error) {
	var missing string
	out := interpToken.ReplaceAllStringFunc(src, func(tok string) string {
		name := tok[2 : len(tok)-2]
		v, ok := bound[name]
		if !ok {
			missing = name
			return tok
		}
		return regexp.QuoteMeta(string(v))
	})
	if missing != "" {
		return "", fmt.Errorf("interpolation {{%s}} not bound by an earlier site", missing)
	}
	return out, nil
}

// expand renders a Replace template, substituting each {{name}} with its raw
// bytes rather than the QuoteMeta form interpolate produces for patterns.
func expand(src string, vals map[string][]byte) ([]byte, error) {
	var missing string
	out := interpToken.ReplaceAllStringFunc(src, func(tok string) string {
		name := tok[2 : len(tok)-2]
		v, ok := vals[name]
		if !ok {
			missing = name
			return tok
		}
		return string(v)
	})
	if missing != "" {
		return nil, fmt.Errorf("replace {{%s}} is neither a group in this pattern nor bound by an earlier site", missing)
	}
	return []byte(out), nil
}

// Validate checks the spec statically: every {{name}} is bound by an earlier
// site, every pattern compiles, and every group ref resolves. It cannot run the
// derive (the interpolated values aren't known until match time), so it probes
// each pattern with placeholder literals — QuoteMeta injects no groups, so the
// group set is identical to the real interpolated pattern.
func (spec DeriveSpec) Validate() error {
	seen := map[string]bool{}
	for i, s := range spec.Sites {
		for _, tok := range interpToken.FindAllStringSubmatch(s.PatternSrc, -1) {
			if !seen[tok[1]] {
				return fmt.Errorf("derive site %d (%q): {{%s}} not bound by an earlier site", i, s.Anchor, tok[1])
			}
		}
		probe := interpToken.ReplaceAllString(s.PatternSrc, "x")
		re, err := regexp.Compile(probe)
		if err != nil {
			return fmt.Errorf("derive site %d (%q): pattern does not compile: %w", i, s.Anchor, err)
		}
		if _, err := s.Find.index(re); err != nil {
			return fmt.Errorf("derive site %d (%q): find %w", i, s.Anchor, err)
		}
		switch {
		case s.Drop != nil && s.Replace != "":
			return fmt.Errorf("derive site %d (%q): sets both drop and replace", i, s.Anchor)
		case s.Drop != nil:
			if _, err := s.Drop.index(re); err != nil {
				return fmt.Errorf("derive site %d (%q): drop %w", i, s.Anchor, err)
			}
		case s.Replace != "":
			for _, tok := range interpToken.FindAllStringSubmatch(s.Replace, -1) {
				if re.SubexpIndex(tok[1]) < 0 && !seen[tok[1]] {
					return fmt.Errorf("derive site %d (%q): replace {{%s}} is neither a group in this pattern nor bound by an earlier site", i, s.Anchor, tok[1])
				}
			}
		default:
			return fmt.Errorf("derive site %d (%q): sets neither drop nor replace", i, s.Anchor)
		}
		for _, name := range s.Bind {
			if re.SubexpIndex(name) < 0 {
				return fmt.Errorf("derive site %d (%q): bind group %q not in pattern", i, s.Anchor, name)
			}
			seen[name] = true
		}
	}
	return nil
}

// DeriveFunc compiles the spec into a Patch.Derive closure. Each call evaluates
// the sites in order against the window, threading a binding table so cross-site
// pins resolve. It errors unless every pattern matches exactly once and every
// drop is a substring of its find (so blank never panics).
func (spec DeriveSpec) DeriveFunc() func([]byte) ([]Site, error) {
	sites := spec.Sites
	return func(window []byte) ([]Site, error) {
		bound := map[string][]byte{}
		out := make([]Site, 0, len(sites))
		for _, s := range sites {
			src, err := interpolate(s.PatternSrc, bound)
			if err != nil {
				return nil, fmt.Errorf("derive %q: %w", s.Anchor, err)
			}
			re, err := regexp.Compile(src)
			if err != nil {
				return nil, fmt.Errorf("derive %q: compile pattern: %w", s.Anchor, err)
			}
			matches := re.FindAllSubmatchIndex(window, 2)
			if len(matches) != 1 {
				return nil, fmt.Errorf("derive %q: pattern matched %d times (expected 1)", s.Anchor, len(matches))
			}
			m := matches[0]
			find, err := s.Find.resolve(re, window, m)
			if err != nil {
				return nil, fmt.Errorf("derive %q find: %w", s.Anchor, err)
			}
			var drop, replace []byte
			if s.Drop != nil {
				if drop, err = s.Drop.resolve(re, window, m); err != nil {
					return nil, fmt.Errorf("derive %q drop: %w", s.Anchor, err)
				}
				if !bytes.Contains(find, drop) {
					return nil, fmt.Errorf("derive %q: drop %q is not within find %q", s.Anchor, drop, find)
				}
			} else {
				vals := map[string][]byte{}
				for k, v := range bound {
					vals[k] = v
				}
				for gi, name := range re.SubexpNames() {
					if name != "" && m[2*gi] >= 0 {
						vals[name] = window[m[2*gi]:m[2*gi+1]]
					}
				}
				if replace, err = expand(s.Replace, vals); err != nil {
					return nil, fmt.Errorf("derive %q: %w", s.Anchor, err)
				}
				if len(replace) != len(find) {
					return nil, fmt.Errorf("derive %q: replace (%d bytes) and find (%d bytes) differ in length", s.Anchor, len(replace), len(find))
				}
			}
			for _, name := range s.Bind {
				idx := re.SubexpIndex(name)
				start, end := m[2*idx], m[2*idx+1]
				if start < 0 || start == end {
					return nil, fmt.Errorf("derive %q: bind group %q matched empty", s.Anchor, name)
				}
				bound[name] = bytes.Clone(window[start:end])
			}
			site := Site{Anchor: s.Anchor + " (derived)", Find: bytes.Clone(find)}
			if s.Drop != nil {
				site.Drop = bytes.Clone(drop)
			} else {
				site.Replace = replace
			}
			out = append(out, site)
		}
		return out, nil
	}
}
