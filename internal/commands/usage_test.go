package commands

import (
	"strings"
	"testing"
)

const usageBody = `{"token":{"name":"read-only probe","read_only":true,"masked_token":"iv_live_ro_…4f2a",
  "status":"active","created_at":"2026-08-01T10:00:00Z","last_used_at":"2026-09-12T09:14:00Z"},
 "groups":{
   "metadata":{"metered":false,"limit":null,"used":null,"remaining":null,"resets_at":null,
     "rate_per_min":120,"source":"plan","degraded":false},
   "current":{"metered":true,"limit":100000,"used":138,"remaining":99862,
     "resets_at":"2026-10-11T12:30:25Z","rate_per_min":300,"source":"balance","degraded":false},
   "history":{"metered":true,"limit":20000,"used":12,"remaining":19988,
     "resets_at":"2026-10-11T12:30:25Z","rate_per_min":300,"source":"balance","degraded":false},
   "reports":{"metered":false,"limit":null,"used":null,"remaining":null,"resets_at":null,
     "rate_per_min":30,"source":"plan","degraded":false}}}`

// ⚠️ An uncounted group has no limit, no used and no remaining. Rendering any
// of those as 0 would read as "you are out", which is the opposite of true.
func TestUsageRendersAnUncountedGroupAsUnknownNotAsZero(t *testing.T) {
	res := run(t, freeJSON(usageBody), "usage")
	if res.err != nil {
		t.Fatalf("usage: %v", res.err)
	}

	line := rowFor(t, res.stdout, "metadata")
	if strings.Contains(line, " 0 ") {
		t.Errorf("an uncounted group printed a zero: %q", line)
	}
	if !strings.Contains(line, "free") || !strings.Contains(line, "—") {
		t.Errorf("metadata line = %q; want it marked free with em dashes", line)
	}

	current := rowFor(t, res.stdout, "current")
	contains(t, current, "METERED")
	contains(t, current, "138")
	contains(t, current, "99862")
}

// The group order is fixed, so two runs of the command can be diffed and a
// figure can be quoted by position.
func TestUsageOrdersTheMeteredGroupsFirst(t *testing.T) {
	res := run(t, freeJSON(usageBody), "usage")
	if res.err != nil {
		t.Fatalf("usage: %v", res.err)
	}
	order := []string{"current", "history", "reports", "metadata"}
	at := -1
	for _, name := range order {
		i := strings.Index(res.stdout, "\n"+name)
		if i < 0 {
			t.Fatalf("group %q is missing from the table\n%s", name, res.stdout)
		}
		if i < at {
			t.Errorf("group %q is out of order", name)
		}
		at = i
	}
}

func TestUsageIsItselfFreeAndSaysWhichCommandsAreNot(t *testing.T) {
	res := run(t, freeJSON(usageBody), "usage")
	if res.err != nil {
		t.Fatalf("usage: %v", res.err)
	}
	contains(t, res.stdout, "cost: FREE — group metadata, uncounted")
	contains(t, res.stdout, "Only stats current")
	contains(t, res.stdout, `Token "read-only probe"`)
	contains(t, res.stdout, "read-only")
}

func TestUsageWarnsWhenTheMeteringStoreWasUnreachable(t *testing.T) {
	body := strings.Replace(usageBody,
		`"rate_per_min":300,"source":"balance","degraded":false}`,
		`"rate_per_min":300,"source":"balance","degraded":true}`, 1)

	res := run(t, freeJSON(body), "usage")
	if res.err != nil {
		t.Fatalf("usage: %v", res.err)
	}
	contains(t, res.stdout, "degraded: the metering store was unreachable")
}
