package heal

import (
	"testing"

	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/registry"
)

// TestValidateRejectsDropNotInFind proves the heal path rejects Claude-derived
// sites whose drop is not within find before Substitutions calls blank(), which
// would otherwise panic the process (and the daily-heal daemon).
func TestValidateRejectsDropNotInFind(t *testing.T) {
	sites := []registry.Site{{Anchor: "x", Find: []byte("hello"), Drop: []byte("world")}}
	if err := validate(claude.Install{}, registry.Patch{}, sites); err == nil {
		t.Fatal("expected validate to reject a drop that is not within find")
	}
}
