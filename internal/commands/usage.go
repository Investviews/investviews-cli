package commands

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/investviews/investviews-cli/internal/api"
)

// groupOrder is the order the groups are printed in: the two that cost money
// first, then the two that do not. Reading a map in Go is unordered, and a
// table whose rows moved between runs would be unreadable to a person and
// unquotable by an agent.
var groupOrder = map[string]int{"current": 0, "history": 1, "reports": 2, "metadata": 3}

// NewUsageCmd builds "investviews usage".
func NewUsageCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "What this token has spent and has left (free)",
		Long: `What this token's account has spent, and what is left, per endpoint group.

This command is FREE and uncounted — checking whether you can still afford a
call never costs one.

⚠️ An uncounted group has NO limit, NO used and NO remaining. They print as an
em dash, never as zero: "0 remaining" on a free group would read as "you are
out", which is the opposite of the truth.

"current" and "history" are separate budgets: spending one does not touch the
other.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}
	v := addJSONFlag(cmd)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := deps.client()
		if err != nil {
			return err
		}
		resp, err := client.Usage(cmd.Context())
		if err != nil {
			return err
		}
		return v.emit(cmd, resp, resp.Meta, 1, api.EndpointUsage, func(w io.Writer) {
			renderUsage(w, resp)
		})
	}
	return cmd
}

func renderUsage(w io.Writer, resp *api.UsageResponse) {
	token := resp.Token
	access := "read/write"
	if token.ReadOnly {
		access = "read-only"
	}
	fmt.Fprintf(w, "Token %q (%s) — %s, %s\n", token.Name, dash(token.MaskedToken), dash(token.Status), access)
	if token.LastUsedAt != nil {
		fmt.Fprintf(w, "last used %s\n", *token.LastUsedAt)
	}
	fmt.Fprintln(w)

	names := make([]string, 0, len(resp.Groups))
	for name := range resp.Groups {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		oi, iKnown := groupOrder[names[i]]
		oj, jKnown := groupOrder[names[j]]
		switch {
		case iKnown && jKnown:
			return oi < oj
		case iKnown != jKnown:
			return iKnown
		default:
			return names[i] < names[j]
		}
	})

	var degraded bool
	table(w, func(tw io.Writer) {
		fmt.Fprintln(tw, "GROUP\tCOST\tUSED\tLIMIT\tREMAINING\tRESETS AT\tRATE/MIN\tSOURCE")
		for _, name := range names {
			g := resp.Groups[name]
			degraded = degraded || g.Degraded
			cost := "free"
			if g.Metered {
				cost = "METERED"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
				name, cost, count(g.Used), count(g.Limit), count(g.Remaining),
				str(g.ResetsAt), g.RatePerMin, dash(g.Source))
		}
	})

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Only stats current (group \"current\") and stats history (group \"history\") spend")
	fmt.Fprintln(w, "quota. Everything under geo, plus coverage and this command, is uncounted.")
	if degraded {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "⚠️ degraded: the metering store was unreachable, so these figures are the")
		fmt.Fprintln(w, "committed balance alone and may lag the truth. Requests are still being served.")
	}
}

// count renders an optional counter. ⚠️ nil means the group is uncounted, so
// it must never render as 0 — "0 remaining" reads as "spent", which is the
// opposite of what an uncounted group means.
func count(v *int64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *v)
}
