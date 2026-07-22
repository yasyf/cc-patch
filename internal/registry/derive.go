package registry

import (
	"fmt"
	"regexp"
)

// Stable anchors survive minification: `.fastMode` is a real property name and
// `="fast"` a literal value, so both persist across releases even as the
// surrounding locals (vl, UO, pAe, BT, gn, Dn, i, ne) get renamed. siteAPattern
// captures the shared eligibility-gate prefix (group 1) plus the service-tier
// site; that prefix then pins the beta-header site.
var siteAPattern = regexp.MustCompile(`((?:\w+\(\)&&){2}!\w+\(\)&&\w+\(\w+\))&&!!(\w+)\.fastMode\)\w+="fast"`)

// deriveFastMode re-locates both fast-mode gates structurally from the bundle
// window, tolerating local-variable renames across a Claude Code update.
func deriveFastMode(window []byte) ([]Site, error) {
	aMatches := siteAPattern.FindAllSubmatch(window, 2)
	if len(aMatches) != 1 {
		return nil, fmt.Errorf("derive: service-tier gate matched %d times (expected 1)", len(aMatches))
	}
	a := aMatches[0]
	gatePrefix := a[1]
	optsA := a[2]

	siteA := Site{
		Anchor: "service tier (derived)",
		Find:   a[0],
		Drop:   append(append([]byte("&&!!"), optsA...), []byte(".fastMode")...),
	}

	bPattern, err := regexp.Compile(`=(` + regexp.QuoteMeta(string(gatePrefix)) + `)&&!!(\w+)\.fastMode`)
	if err != nil {
		return nil, fmt.Errorf("derive: build beta-header pattern: %w", err)
	}
	bMatches := bPattern.FindAllSubmatch(window, 2)
	if len(bMatches) != 1 {
		return nil, fmt.Errorf("derive: beta-header gate matched %d times (expected 1)", len(bMatches))
	}
	b := bMatches[0]
	optsB := b[2]

	siteB := Site{
		Anchor: "fast-mode beta header (derived)",
		Find:   b[0],
		Drop:   append(append([]byte("&&!!"), optsB...), []byte(".fastMode")...),
	}

	return []Site{siteA, siteB}, nil
}
