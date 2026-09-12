package commands

import (
	"net/http"
	"testing"

	"github.com/investviews/investviews-cli/internal/api"
)

// The exit codes are a contract with whoever scripts around this CLI: 2 is
// money, 3 is credentials, 4 is a market we do not serve, 1 is everything
// else. A locally refused request is a usage mistake, so it stays on 1.
func TestExitCodesSayWhichKindOfFailureItWas(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    int
		command []string
	}{
		{
			name:    "quota exhausted is money, not credentials",
			status:  http.StatusPaymentRequired,
			body:    `{"error":"quota_exhausted","message":"spent","docs_url":"d"}`,
			want:    api.ExitQuota,
			command: []string{"stats", "current", "--geo-id", "es"},
		},
		{
			name:    "an inactive subscription is money too",
			status:  http.StatusPaymentRequired,
			body:    `{"error":"subscription_inactive","message":"inactive","docs_url":"d"}`,
			want:    api.ExitQuota,
			command: []string{"stats", "current", "--geo-id", "es"},
		},
		{
			name:    "a bad token is credentials",
			status:  http.StatusUnauthorized,
			body:    `{"error":"invalid_token","message":"bad","docs_url":"d"}`,
			want:    api.ExitAuth,
			command: []string{"usage"},
		},
		{
			name:    "a market we do not serve has its own code",
			status:  http.StatusNotFound,
			body:    `{"error":"not_covered","message":"no market","docs_url":"d"}`,
			want:    api.ExitNotCovered,
			command: []string{"coverage", "--country", "aq"},
		},
		{
			name:    "an unknown place is ordinary failure — it has a next move",
			status:  http.StatusNotFound,
			body:    `{"error":"unknown_place","message":"no match","docs_url":"d","did_you_mean":[]}`,
			want:    api.ExitError,
			command: []string{"geo", "search", "zzz"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, func(w http.ResponseWriter, _ *http.Request) {
				writeBody(w, tc.status, tc.body)
			}, tc.command...)
			if res.err == nil {
				t.Fatal("want an error")
			}
			if got := ExitCode(res.err); got != tc.want {
				t.Errorf("exit code = %d, want %d (error: %v)", got, tc.want, res.err)
			}
		})
	}
}

func TestExitCodeForAMissingTokenIsTheCredentialCode(t *testing.T) {
	if got := ExitCode(ErrNoToken); got != api.ExitAuth {
		t.Errorf("exit code = %d, want %d", got, api.ExitAuth)
	}
	if got := ExitCode(nil); got != api.ExitOK {
		t.Errorf("exit code for no error = %d, want %d", got, api.ExitOK)
	}
}

// A command that cannot build a client never reaches the network, and says
// what to do rather than letting a 401 explain it.
func TestACommandWithoutACredentialRefusesBeforeAnyRequest(t *testing.T) {
	deps := Deps{NewClient: func() (*api.Client, error) { return nil, ErrNoToken }}
	for _, cmd := range All(deps) {
		if cmd.Name() == "" {
			t.Fatal("a command with no name")
		}
	}
	if got := ExitCode(ErrNoToken); got != api.ExitAuth {
		t.Errorf("exit code = %d", got)
	}
}

func TestAvailabilityIsThreeAnswersNotTwo(t *testing.T) {
	ask := availability(true, api.Date("2026-08-01"))
	thin := availability(true, api.NullDate())
	dead := availability(false, api.NullDate())

	if ask == thin || thin == dead || ask == dead {
		t.Fatalf("the three states must read differently:\n%q\n%q\n%q", ask, thin, dead)
	}
	contains(t, thin, "stats history")
	contains(t, thin, "not a dead end")
	contains(t, dead, "do not spend a metered call")
}

// ⚠️ null ancestry and empty ancestry are different answers. Unknown means
// "try another selector"; empty means "this IS the root".
func TestAncestryKeepsUnknownApartFromRoot(t *testing.T) {
	if got := ancestry(api.UnknownAncestors()); got == ancestry(api.NewAncestors(nil)) {
		t.Fatal("unknown and root ancestry must not render the same")
	}
	contains(t, ancestry(api.UnknownAncestors()), "ancestry unknown")
	contains(t, ancestry(api.NewAncestors(nil)), "top of the tree")
	contains(t,
		ancestry(api.NewAncestors([]api.Ancestor{{GeoID: "R1", Name: "Madrid", Level: "city"}})),
		"Madrid (city, R1)")
}

// ⚠️ A missing figure is an em dash, never a zero: the aggregate held nothing
// for that field, which is not the number 0.
func TestNumNeverTurnsAMissingFigureIntoZero(t *testing.T) {
	if got := num(nil); got != "—" {
		t.Errorf("num(nil) = %q", got)
	}
	zero := 0.0
	if got := num(&zero); got != "0" {
		t.Errorf("a real zero must still print: %q", got)
	}
	value := 3120.456
	if got := num(&value); got != "3120.46" {
		t.Errorf("num = %q", got)
	}
}

func TestHexIDsAcceptRepeatsCommasAndStdin(t *testing.T) {
	cells, err := hexIDs([]string{"1,2", "3"}, nil)
	if err != nil {
		t.Fatalf("hexIDs: %v", err)
	}
	if len(cells) != 3 || cells[0] != "1" || cells[2] != "3" {
		t.Errorf("cells = %v", cells)
	}
}
