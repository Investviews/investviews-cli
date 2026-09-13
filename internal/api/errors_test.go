package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Every code the contract declares, mapped to its type, its exit code and its
// sentinel. A code that grows a new meaning fails here first.
func TestEveryErrorCodeMapsToATypeAndAnExitCode(t *testing.T) {
	cases := []struct {
		code     ErrorCode
		status   int
		sentinel error
		exit     int
		// wantType is a function that reports whether err is the expected
		// concrete type.
		wantType func(error) bool
		typeName string
	}{
		{CodeInvalidToken, 401, ErrInvalidToken, ExitAuth, isType[*AuthError], "*AuthError"},
		{CodeReadOnlyToken, 403, ErrReadOnlyToken, ExitAuth, isType[*AuthError], "*AuthError"},
		{CodeSubscriptionInactive, 402, ErrSubscriptionInactive, ExitQuota, isType[*QuotaError], "*QuotaError"},
		{CodeQuotaExhausted, 402, ErrQuotaExhausted, ExitQuota, isType[*QuotaError], "*QuotaError"},
		{CodeNotCovered, 404, ErrNotCovered, ExitNotCovered, isType[*NotCoveredError], "*NotCoveredError"},
		{CodeUnknownPlace, 404, ErrUnknownPlace, ExitError, isType[*UnknownPlaceError], "*UnknownPlaceError"},
		{CodeInvalidCursor, 400, ErrInvalidCursor, ExitError, isType[*InvalidCursorError], "*InvalidCursorError"},
		{CodeRateLimited, 429, ErrRateLimited, ExitError, isType[*RateLimitError], "*RateLimitError"},
		{CodeInvalidParameter, 400, ErrInvalidParameter, ExitError, isType[*APIError], "*APIError"},
		{CodeMissingParameter, 400, ErrMissingParameter, ExitError, isType[*APIError], "*APIError"},
		{CodeResolutionNotInPlan, 403, ErrResolutionNotInPlan, ExitError, isType[*APIError], "*APIError"},
		{CodeTooManyHexes, 422, ErrTooManyHexes, ExitError, isType[*APIError], "*APIError"},
		{CodeReportsUnavailableForCountry, 422, ErrReportsUnavailableForCountry, ExitError, isType[*APIError], "*APIError"},
		{CodeNotFound, 404, ErrNotFound, ExitError, isType[*APIError], "*APIError"},
		{CodeMalformedRequest, 400, ErrMalformedRequest, ExitError, isType[*APIError], "*APIError"},
		{CodeInternalError, 500, ErrInternalError, ExitError, isType[*APIError], "*APIError"},
	}

	if len(cases) != 16 {
		t.Fatalf("the contract declares 16 codes, the table covers %d", len(cases))
	}

	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			body := `{"error":"` + string(tc.code) + `","message":"the server said this","docs_url":"https://docs.investviews.ai/errors.html"}`
			rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				freeHeaders(w)
				writeJSON(w, tc.status, body)
			}, func(o *Options) { o.MaxRetries = -1 })

			_, err := rec.client.Usage(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is(err, %s) = false", tc.code)
			}
			if !tc.wantType(err) {
				t.Errorf("err = %T, want %s", err, tc.typeName)
			}
			if got := ExitCode(err); got != tc.exit {
				t.Errorf("ExitCode = %d, want %d", got, tc.exit)
			}

			apiErr, ok := AsAPIError(err)
			if !ok {
				t.Fatalf("AsAPIError failed for %T", err)
			}
			// The server's sentence, verbatim.
			if apiErr.Message != "the server said this" {
				t.Errorf("Message = %q — the server's message must not be rewritten", apiErr.Message)
			}
			if apiErr.DocsURL != "https://docs.investviews.ai/errors.html" {
				t.Errorf("DocsURL = %q — every envelope carries it and it must not be dropped", apiErr.DocsURL)
			}
			if !strings.Contains(err.Error(), "the server said this") {
				t.Errorf("Error() = %q, must carry the server's message", err.Error())
			}
			if !strings.Contains(err.Error(), apiErr.DocsURL) {
				t.Errorf("Error() = %q, must carry docs_url", err.Error())
			}
		})
	}
}

func isType[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

// ⚠️ invalid_parameter and invalid_cursor must never be conflated: a client
// that treats the second as the first retries the same rejected cursor forever.
func TestInvalidCursorIsNotAnInvalidParameter(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 400, `{"error":"invalid_cursor","parameter":"cursor","message":"That cursor was issued for a different query. Start the listing again without `+"`cursor`"+`.","docs_url":"d"}`)
	})

	_, err := rec.client.PlaceHexes(context.Background(), "R344953", HexesParams{Cursor: "someone-elses"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrInvalidParameter) {
		t.Error("invalid_cursor must NOT match the invalid_parameter sentinel")
	}
	if !errors.Is(err, ErrInvalidCursor) {
		t.Error("invalid_cursor must match its own sentinel")
	}
	var cursorErr *InvalidCursorError
	if !errors.As(err, &cursorErr) {
		t.Fatalf("err = %T, want *InvalidCursorError", err)
	}
	if cursorErr.Parameter != "cursor" {
		t.Errorf("Parameter = %q", cursorErr.Parameter)
	}
	if !strings.Contains(cursorErr.Hint(), "WITHOUT a cursor") {
		t.Errorf("Hint = %q, must say to restart the listing", cursorErr.Hint())
	}
	if rec.hits() != 1 {
		t.Errorf("hits = %d — a rejected cursor must never be retried", rec.hits())
	}
}

// ⚠️ unknown_place has a next move (did_you_mean); not_covered never succeeds.
func TestUnknownPlaceAndNotCoveredAreDifferentAnswers(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 404, `{"error":"unknown_place","message":"Nothing matched.","docs_url":"d",`+
			`"query":"zzzznotaplace","did_you_mean":[`+
			`{"geo_id":"R1","name":"Zaragoza","level":"city","country":"es","ancestors":[]},`+
			`{"geo_id":"R2","name":"Zamora","level":"city","country":"es","ancestors":[]}]}`)
	})
	_, err := rec.client.GeoSearch(context.Background(), SearchParams{Q: "zzzznotaplace"})
	var unknown *UnknownPlaceError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %T, want *UnknownPlaceError", err)
	}
	if len(unknown.DidYouMean) != 2 || unknown.DidYouMean[0].Name != "Zaragoza" {
		t.Errorf("DidYouMean = %v — the suggestions are the next move", unknown.DidYouMean)
	}
	if unknown.DidYouMean[0].GeoID != "R1" {
		t.Errorf("geo_id = %q — the id is what makes a suggestion actionable", unknown.DidYouMean[0].GeoID)
	}
	if unknown.Query != "zzzznotaplace" {
		t.Errorf("Query = %q", unknown.Query)
	}
	if ExitCode(err) == ExitNotCovered {
		t.Error("unknown_place must NOT exit as not_covered: one has a retry, the other never does")
	}

	rec2 := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 404, `{"error":"not_covered","message":"No geographic data for that market.","docs_url":"d"}`)
	})
	_, err2 := rec2.client.GeoSearch(context.Background(), SearchParams{Q: "somewhere", Country: "zz"})
	var notCovered *NotCoveredError
	if !errors.As(err2, &notCovered) {
		t.Fatalf("err = %T, want *NotCoveredError", err2)
	}
	if ExitCode(err2) != ExitNotCovered {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err2), ExitNotCovered)
	}
	if !strings.Contains(notCovered.Hint(), "never succeeds") {
		t.Errorf("Hint = %q", notCovered.Hint())
	}
}

// ── the two 429s ─────────────────────────────────────────────────────────────

// Both carry error: "rate_limited" ON PURPOSE, so the branch is on the extra
// fields. The app layer adds group/limit/retry_after and the quota headers;
// nginx's edge body has none of them and Rails never ran.
func TestTheTwoRateLimitsAreToldApartByTheirExtraFields(t *testing.T) {
	const appBody = `{"error":"rate_limited",` +
		`"message":"Too many requests: this account is limited to 300 requests per minute on the current endpoint group. Retry in 17 seconds.",` +
		`"docs_url":"https://docs.investviews.ai/errors.html","retry_after":17,"limit":300,"group":"current"}`

	const edgeBody = `{"error":"rate_limited",` +
		`"message":"Too many requests from this address. This is a per-address edge limit, not your account quota — retry in a second. If you are seeing this at a low request rate, check whether you are sharing an address with other clients.",` +
		`"docs_url":"https://docs.investviews.ai/errors.html"}`

	t.Run("app layer", func(t *testing.T) {
		rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "17")
			meteredHeaders(w)
			writeJSON(w, 429, appBody)
		}, func(o *Options) { o.MaxRetries = -1 })

		_, err := rec.client.StatsCurrent(context.Background(), StatsParams{GeoID: "es"})
		var rate *RateLimitError
		if !errors.As(err, &rate) {
			t.Fatalf("err = %T, want *RateLimitError", err)
		}
		if rate.Scope != RateLimitAccount {
			t.Errorf("Scope = %q, want %q", rate.Scope, RateLimitAccount)
		}
		if rate.Group != "current" || rate.Limit == nil || *rate.Limit != 300 {
			t.Errorf("group = %q limit = %v — the app layer names both", rate.Group, rate.Limit)
		}
		if rate.Wait != 17*time.Second || !rate.HasRetry {
			t.Errorf("Wait = %v", rate.Wait)
		}
		// A 429 is NEVER a quota problem — that is 402 quota_exhausted.
		if ExitCode(err) == ExitQuota {
			t.Error("a 429 must not exit as a quota failure: it sends the user to buy what they already have")
		}
		if !strings.Contains(rate.Hint(), "NOT your quota") {
			t.Errorf("Hint = %q", rate.Hint())
		}
	})

	t.Run("nginx edge", func(t *testing.T) {
		rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			// nginx generates this itself: Retry-After only, and none of
			// the X-Quota-* / X-RateLimit-* block.
			w.Header().Set("Retry-After", "1")
			writeJSON(w, 429, edgeBody)
		}, func(o *Options) { o.MaxRetries = -1 })

		_, err := rec.client.StatsCurrent(context.Background(), StatsParams{GeoID: "es"})
		var rate *RateLimitError
		if !errors.As(err, &rate) {
			t.Fatalf("err = %T, want *RateLimitError", err)
		}
		if rate.Scope != RateLimitEdge {
			t.Errorf("Scope = %q, want %q", rate.Scope, RateLimitEdge)
		}
		if rate.Group != "" || rate.Limit != nil || rate.RetryAfter != nil {
			t.Error("the edge body carries no group, limit or retry_after")
		}
		if rate.Meta.HasQuotaHeaders() {
			t.Error("the edge response carries no quota headers")
		}
		if rate.Wait != time.Second {
			t.Errorf("Wait = %v, want 1s from the Retry-After header", rate.Wait)
		}
		if ExitCode(err) == ExitQuota {
			t.Error("the edge 429 is not even about this account")
		}
	})
}

// Both 429s share one code, so a client needs ONE branch for "slow down".
func TestBothRateLimitsShareTheCodeAndTheSentinel(t *testing.T) {
	for _, body := range []string{
		`{"error":"rate_limited","message":"app","docs_url":"d","retry_after":3,"limit":300,"group":"current"}`,
		`{"error":"rate_limited","message":"edge","docs_url":"d"}`,
	} {
		rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 429, body)
		}, func(o *Options) { o.MaxRetries = -1 })
		_, err := rec.client.Usage(context.Background())
		if !errors.Is(err, ErrRateLimited) {
			t.Errorf("body %q did not match ErrRateLimited", body)
		}
	}
}

func TestAppLayerRetryAfterFallsBackToTheBodyField(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		// No Retry-After header, only the body field.
		meteredHeaders(w)
		writeJSON(w, 429, `{"error":"rate_limited","message":"slow down","docs_url":"d","retry_after":5,"limit":300,"group":"current"}`)
	}, func(o *Options) { o.MaxRetries = -1 })

	_, err := rec.client.Usage(context.Background())
	var rate *RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("err = %T", err)
	}
	if rate.Wait != 5*time.Second || !rate.HasRetry {
		t.Errorf("Wait = %v, want 5s from the body's retry_after", rate.Wait)
	}
}

// ── local refusals ───────────────────────────────────────────────────────────

// The API refuses two selectors; the CLI refuses them first, so it costs no
// request and no quota.
func TestASelectorConflictIsRefusedWithoutARequest(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this request should never have been sent")
	})

	_, err := rec.client.StatsCurrent(context.Background(), StatsParams{
		GeoID: "R344953",
		Lat:   ptrFloat(39.46), Lng: ptrFloat(-0.37), RadiusKm: ptrFloat(5),
	})
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("err = %T, want *ValidationError", err)
	}
	if len(valErr.Selectors) != 2 {
		t.Errorf("Selectors = %v, want the two that collided", valErr.Selectors)
	}
	if rec.hits() != 0 {
		t.Errorf("hits = %d, want 0 — a local refusal costs no request", rec.hits())
	}
	if ExitCode(err) != ExitError {
		t.Errorf("ExitCode = %d", ExitCode(err))
	}
}

func TestTerritorySelectorRules(t *testing.T) {
	cases := []struct {
		name    string
		params  StatsParams
		wantErr bool
	}{
		{"geo_id alone", StatsParams{GeoID: "es"}, false},
		{"h3 alone", StatsParams{H3: []string{"613498079267520511"}}, false},
		{"circle", StatsParams{Lat: ptrFloat(1), Lng: ptrFloat(2), RadiusKm: ptrFloat(3)}, false},
		{"geo_id with res", StatsParams{GeoID: "es", Res: ptrInt(8)}, true},
		{"h3 with res", StatsParams{H3: []string{"1"}, Res: ptrInt(8)}, true},
		{"circle with res", StatsParams{Lat: ptrFloat(1), Lng: ptrFloat(2), RadiusKm: ptrFloat(3), Res: ptrInt(8)}, false},
		{"half a circle", StatsParams{Lat: ptrFloat(1), Lng: ptrFloat(2)}, true},
		{"no selector", StatsParams{}, true},
		{"h3 and geo_id", StatsParams{H3: []string{"1"}, GeoID: "es"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.params.selector()
			if tc.wantErr != (err != nil) {
				t.Fatalf("selector() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ⚠️ did_you_mean is an array of OBJECTS, not names.
//
// It was typed []string until 2026-09-12. The EMPTY case decodes into []string
// perfectly, and the empty case was all the ground truth recorded, so nothing
// looked wrong until a live miss returned objects — and then the strict
// one-pass envelope decode failed outright, reporting every unknown_place as
// not_found with no message, no hint and no suggestions.
//
// This pins all three halves: the objects decode, the ids survive, and the
// envelope around them is unharmed.
func TestDidYouMeanCarriesObjectsWithSpendableIDs(t *testing.T) {
	// The live shape, from api.investviews.ai on 2026-09-12.
	const body = `{"error":"unknown_place",` +
		`"message":"No place matches \"springfield\" in jp.",` +
		`"docs_url":"https://docs.investviews.ai/errors.html",` +
		`"query":"springfield",` +
		`"did_you_mean":[` +
		`{"geo_id":"N386190007","name":"Springfield","level":"microzone","country":"gb",` +
		`"ancestors":[{"geo_id":"N731914594","name":"Wake Green","level":"macrozone"},` +
		`{"geo_id":"R162378","name":"Birmingham","level":"city"},` +
		`{"geo_id":"R58447","name":"England","level":"region"}]}]}`

	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 404, body)
	})

	_, err := rec.client.GeoSearch(context.Background(), SearchParams{Q: "springfield", Country: "jp"})
	if err == nil {
		t.Fatal("a search miss is a 404")
	}

	// The envelope survives: the right code, not the not_found fallback.
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("err = %T, want an *APIError", err)
	}
	if apiErr.Code != CodeUnknownPlace {
		t.Fatalf("code = %q, want unknown_place — a suggestion's shape must never cost the envelope", apiErr.Code)
	}
	if !errors.Is(err, ErrUnknownPlace) {
		t.Error("the sentinel must match")
	}
	if !strings.Contains(apiErr.Message, "springfield") {
		t.Errorf("message = %q — the server's sentence must survive", apiErr.Message)
	}
	if apiErr.Query != "springfield" {
		t.Errorf("query = %q", apiErr.Query)
	}
	if !strings.Contains(apiErr.Hint(), "did_you_mean") {
		t.Errorf("hint = %q — the next move must reach the user", apiErr.Hint())
	}

	// The suggestions survive, ids and all.
	var unknown *UnknownPlaceError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %T, want *UnknownPlaceError", err)
	}
	if len(unknown.DidYouMean) != 1 {
		t.Fatalf("suggestions = %d, want 1", len(unknown.DidYouMean))
	}
	hit := unknown.DidYouMean[0]
	if hit.GeoID != "N386190007" {
		t.Errorf("geo_id = %q — the id is the whole value of a suggestion", hit.GeoID)
	}
	if hit.Name != "Springfield" || hit.Level != "microzone" || hit.Country != "gb" {
		t.Errorf("suggestion = %+v", hit)
	}
	if !hit.Ancestors.Known() || hit.Ancestors.Len() != 3 {
		t.Fatalf("ancestors = %+v, want the three-step chain", hit.Ancestors.List())
	}
	if hit.Ancestors.List()[0].GeoID != "N731914594" {
		t.Errorf("the chain runs fine to coarse: %+v", hit.Ancestors.List())
	}
	// ⚠️ Country() returns the LAST step, whatever level it is at. This
	// fixture's chain stops at region; a live 2026-09-12 miss ended on a
	// country entry. So read the level, never the position.
	last, _ := hit.Ancestors.Country()
	if last.Level != "region" {
		t.Errorf("last step = %+v; Country() is the last step, not a guaranteed country", last)
	}
}

// The empty case is the one that hid the bug for a day. Keep it.
func TestDidYouMeanEmptyArrayIsStillAnUnknownPlace(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 404, `{"error":"unknown_place","message":"Nothing matched.",`+
			`"docs_url":"https://docs.investviews.ai/errors.html","query":"zzz","did_you_mean":[]}`)
	})

	_, err := rec.client.GeoSearch(context.Background(), SearchParams{Q: "zzz"})
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.Code != CodeUnknownPlace {
		t.Fatalf("err = %v, want unknown_place", err)
	}
	if len(apiErr.DidYouMean) != 0 {
		t.Errorf("DidYouMean = %v, want none", apiErr.DidYouMean)
	}
}

// A field this client cannot read must cost that field and nothing else. The
// envelope's three required keys are what a user actually needs.
func TestAnUnreadableOptionalFieldDoesNotCostTheEnvelope(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		// did_you_mean as bare strings — the shape this client was wrong
		// about once already. It must degrade, not detonate.
		writeJSON(w, 404, `{"error":"unknown_place","message":"Nothing matched.",`+
			`"docs_url":"https://docs.investviews.ai/errors.html","did_you_mean":["valencia"]}`)
	})

	_, err := rec.client.GeoSearch(context.Background(), SearchParams{Q: "valenica"})
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("err = %T", err)
	}
	if apiErr.Code != CodeUnknownPlace {
		t.Errorf("code = %q, want unknown_place", apiErr.Code)
	}
	if apiErr.Message != "Nothing matched." || apiErr.DocsURL == "" {
		t.Errorf("the required fields must survive: %+v", apiErr)
	}
	if len(apiErr.DidYouMean) != 0 {
		t.Errorf("a shape we cannot read yields no suggestions, not wrong ones: %v", apiErr.DidYouMean)
	}
	// The raw bytes are still there for a caller that needs them.
	if _, ok := apiErr.Extra["did_you_mean"]; !ok {
		t.Error("the raw field must stay in Extra")
	}
}

// ⚠️ A HINT THAT NAMES A FLAG THIS CLIENT REFUSES IS A HINT THAT FAILS.
//
// --res is valid for the circle ALONE — StatsParams.selector refuses it
// alongside both h3 and geo_id — and these two codes are most often reached
// through h3: too many cells, or cells finer than the plan. Both hints used to
// say "ask for a coarser --res" and stop, which for an h3 caller is an
// instruction to run a command that exits 1 before it is sent.
func TestHintsDoNotSendAnH3CallerToAFlagThatIsRefusedAlongsideH3(t *testing.T) {
	for _, code := range []ErrorCode{CodeResolutionNotInPlan, CodeTooManyHexes} {
		t.Run(string(code), func(t *testing.T) {
			hint := (&APIError{Code: code}).Hint()
			if !strings.Contains(hint, "--res") {
				t.Fatalf("hint = %q — the circle's move is still --res and must still be named", hint)
			}
			// It must not read as "--res, whatever you asked with".
			for _, want := range []string{"--h3", "--geo-id"} {
				if !strings.Contains(hint, want) {
					t.Errorf("hint = %q — it names --res without saying what %s callers do instead", hint, want)
				}
			}
		})
	}

	// And the rule it rests on: --res really is refused with both.
	for _, params := range []StatsParams{
		{H3: []string{"613498076398616575"}, Res: ptrInt(6)},
		{GeoID: "es", Res: ptrInt(6)},
	} {
		if _, err := params.selector(); err == nil {
			t.Errorf("selector() accepted res alongside %+v; the hints above assume it does not", params)
		}
	}
}
