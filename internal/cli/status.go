package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-patch/internal/binpatch"
	"github.com/yasyf/cc-patch/internal/claude"
	"github.com/yasyf/cc-patch/internal/patcher"
	"github.com/yasyf/cc-patch/internal/patchset"
	"github.com/yasyf/cc-patch/internal/procs"
)

// stamp is the timestamp layout for the binary's write and a process's start.
const stamp = "2006-01-02 15:04:05"

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether each patch is applied to the installed binary, and whether the running processes have it",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := claude.Locate()
			if err != nil {
				return err
			}
			cmd.Printf("claude %s at %s\n", inst.Version, inst.Binary)
			patches, warns, err := patchset.Load(cmd.Context())
			if err != nil {
				return err
			}
			warn(cmd, warns)
			for _, p := range patches {
				out, err := patcher.Status(inst, p)
				if err != nil {
					cmd.Printf("  %s  error: %v\n", p.ID, err)
					continue
				}
				if retired(cmd, p) {
					cmd.Printf("  %s  %s (retired — %s closed)\n", p.ID, summarize(out.Result), p.Upstream)
					continue
				}
				cmd.Printf("  %s  %s\n", p.ID, summarize(out.Result))
			}
			report, err := procs.Inspect(cmd.Context(), inst)
			if err != nil {
				return err
			}
			cmd.Printf("binary last written %s; %d claude processes running\n", report.Written.Format(stamp), len(report.Processes))
			for _, p := range report.Processes {
				cmd.Printf("  pid %d  started %s  %s\n", p.PID, p.Started.Format(stamp), mapping(p))
			}
			if report.Unidentified > 0 {
				cmd.Printf("%d running processes would not name their executable, so a claude process may be missing above\n", report.Unidentified)
			}
			if stale := len(report.Stale()); stale > 0 {
				cmd.Printf("%d of %d running processes do not map this binary, so they do not have the patches above — restart Claude Code\n", stale, len(report.Processes))
			}
			return nil
		},
	}
}

func mapping(p procs.Process) string {
	if p.Current {
		return "current"
	}
	return "stale"
}

func summarize(r binpatch.Result) string {
	patched, unpatched, missing := 0, 0, 0
	for _, s := range r.Sites {
		switch s.State {
		case binpatch.StatePatched:
			patched++
		case binpatch.StateUnpatched:
			unpatched++
		case binpatch.StateMissing:
			missing++
		}
	}
	switch {
	case missing > 0:
		return "drifted"
	case unpatched == 0:
		return "patched"
	case patched == 0:
		return "not patched"
	default:
		return "partial"
	}
}
