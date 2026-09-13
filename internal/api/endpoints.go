package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Endpoint names one live route.
type Endpoint string

// The eleven live endpoints.
const (
	EndpointPing         Endpoint = "/ping"
	EndpointOpenAPI      Endpoint = "/openapi.yaml"
	EndpointGeo          Endpoint = "/geo"
	EndpointGeoSearch    Endpoint = "/geo/search"
	EndpointGeoLookup    Endpoint = "/geo/lookup"
	EndpointGeoHexes     Endpoint = "/geo/{geo_id}/hexes"
	EndpointCoverage     Endpoint = "/coverage"
	EndpointMetaFilters  Endpoint = "/meta/filters"
	EndpointUsage        Endpoint = "/usage"
	EndpointStatsCurrent Endpoint = "/stats/current"
	EndpointStatsHistory Endpoint = "/stats/history"
)

// Endpoints lists every route this client covers, in the contract's order.
func Endpoints() []Endpoint {
	return []Endpoint{
		EndpointPing, EndpointOpenAPI,
		EndpointGeo, EndpointGeoSearch, EndpointGeoLookup, EndpointGeoHexes,
		EndpointCoverage, EndpointMetaFilters, EndpointUsage,
		EndpointStatsCurrent, EndpointStatsHistory,
	}
}

// Group is the quota group this endpoint spends from — what we EXPECT before
// the call. The response's own X-Quota-* headers are what actually happened;
// prefer Meta.Metered over this when you have a response in hand.
func (e Endpoint) Group() string {
	switch e {
	case EndpointStatsCurrent:
		return "current"
	case EndpointStatsHistory:
		return "history"
	default:
		return "metadata"
	}
}

// Metered reports whether this endpoint spends quota. Only the two /stats
// routes do.
func (e Endpoint) Metered() bool {
	return e == EndpointStatsCurrent || e == EndpointStatsHistory
}

// ─────────────────────────────────────────────────────────────────────────────
// service
// ─────────────────────────────────────────────────────────────────────────────

// Ping is the liveness probe. It is sent WITHOUT the Authorization header on
// purpose: the question it answers is "is the service up", and sending a token
// would let a bad credential turn a service outage into a 401.
func (c *Client) Ping(ctx context.Context) (*PingResponse, error) {
	var out PingResponse
	meta, err := c.get(ctx, request{path: string(EndpointPing), anonymous: true}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// OpenAPI fetches the served contract — the only copy that cannot go stale.
// Unauthenticated, so the whole surface is discoverable before a token exists.
func (c *Client) OpenAPI(ctx context.Context) ([]byte, Meta, error) {
	return c.getRaw(ctx, request{
		path:      string(EndpointOpenAPI),
		anonymous: true,
		accept:    "application/openapi+yaml, application/yaml, text/yaml",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// geo — free
// ─────────────────────────────────────────────────────────────────────────────

// GeoParams addresses /geo.
//
// ⚠️ Level means TWO different things. Under a country it selects that whole
// level (parent=es&level=city is every Spanish city, wherever it hangs);
// under a place it filters that place's direct children. Same field, two
// operations — do not paper over it by defaulting Level.
type GeoParams struct {
	// Parent is a country code (es) or a place's OSM ref (R344953). Empty
	// asks for the country list.
	Parent string
	Level  string
	Limit  int
	Page   int
}

func (p GeoParams) values() url.Values {
	v := url.Values{}
	setString(v, "parent", p.Parent)
	setString(v, "level", p.Level)
	setPositive(v, "limit", p.Limit)
	setPositive(v, "page", p.Page)
	return v
}

// GeoBrowse walks the geography one page at a time. Free.
//
// ⚠️ An empty results array is a NORMAL answer, not an error: an unresolvable
// parent, a page past the end, a level a branch does not have — all answer 200
// with results: []. Exploring is meant to be safe.
func (c *Client) GeoBrowse(ctx context.Context, params GeoParams) (*GeoResponse, error) {
	if params.Limit > GeoMaxLimit {
		return nil, &ValidationError{
			Parameter: "limit",
			Message:   fmt.Sprintf("/geo takes a limit of at most %d", GeoMaxLimit),
		}
	}
	if params.Page > GeoMaxPage {
		return nil, &ValidationError{
			Parameter: "page",
			Message:   fmt.Sprintf("/geo takes a page of at most %d", GeoMaxPage),
		}
	}
	var out GeoResponse
	meta, err := c.get(ctx, request{path: string(EndpointGeo), query: params.values()}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// SearchParams addresses /geo/search.
type SearchParams struct {
	Q       string
	Country string
	Level   string
	// Limit defaults to 10 and is CLAMPED at 50 by the server, silently.
	Limit int
}

func (p SearchParams) values() url.Values {
	v := url.Values{}
	setString(v, "q", p.Q)
	setString(v, "country", p.Country)
	setString(v, "level", p.Level)
	setPositive(v, "limit", p.Limit)
	return v
}

// GeoSearch resolves a name into something queryable. Free.
//
// ⚠️ Names are not unique — 13.6% of neighbourhood names repeat inside their
// own country — so expect rows identical on name, level and country.
// Ancestors is what tells them apart, and every entry carries a spendable id.
//
// A miss is a 404 unknown_place carrying did_you_mean; the typed error keeps
// the suggestions.
func (c *Client) GeoSearch(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	if strings.TrimSpace(params.Q) == "" {
		return nil, &ValidationError{
			Parameter: "q",
			Message:   "/geo/search needs a name to resolve; browse from /geo when you do not have one",
		}
	}
	var out SearchResponse
	meta, err := c.get(ctx, request{path: string(EndpointGeoSearch), query: params.values()}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// LookupParams addresses /geo/lookup: one cell, or one coordinate pair.
type LookupParams struct {
	// H3 is a decimal id (613498079267520511) or a canonical H3 string.
	H3 string
	// Lat and Lng are the coordinate form. Res applies only to this form —
	// a cell encodes its own resolution.
	Lat *float64
	Lng *float64
	Res *int
}

// GeoLookup names the zones an H3 cell or a point belongs to. Free.
//
// ⚠️ This is the ONE endpoint whose zones[] can carry ancestors: null. Read
// Ancestors.Known before concluding a zone is at the root of the tree.
func (c *Client) GeoLookup(ctx context.Context, params LookupParams) (*Point, error) {
	hasCell := strings.TrimSpace(params.H3) != ""
	hasPoint := params.Lat != nil || params.Lng != nil
	switch {
	case hasCell && hasPoint:
		return nil, &ValidationError{
			Selectors: []string{"h3", "lat/lng"},
			Message:   "/geo/lookup names one cell OR one point — drop all but one",
		}
	case !hasCell && !hasPoint:
		return nil, &ValidationError{
			Message: "/geo/lookup needs either h3= or lat= and lng=",
		}
	case hasPoint && (params.Lat == nil || params.Lng == nil):
		return nil, &ValidationError{
			Parameter: "lat",
			Message:   "the coordinate form needs both lat and lng",
		}
	}

	// Free endpoint, same check: a token that is not a cell gets a local
	// answer naming it, rather than an unknown_place 404 that reads as "this
	// cell is outside our geography" when the truth is "that was not a cell".
	if hasCell {
		if err := ValidateH3Cells([]string{params.H3}); err != nil {
			return nil, err
		}
	}

	v := url.Values{}
	if hasCell {
		setString(v, "h3", params.H3)
	} else {
		setFloat(v, "lat", params.Lat)
		setFloat(v, "lng", params.Lng)
		setInt(v, "res", params.Res)
	}

	var out Point
	meta, err := c.get(ctx, request{path: string(EndpointGeoLookup), query: v}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// HexesParams addresses one page of /geo/{geo_id}/hexes.
type HexesParams struct {
	// Cursor is the previous page's next_cursor, VERBATIM. It is bound to
	// this exact query: never carry one across places or filter changes.
	Cursor string
	Limit  int
}

// PlaceHexes reads one page of a place's cells. Free.
//
// Prefer PlaceHexesAll or PlaceHexesCollect, which walk the cursor for you.
func (c *Client) PlaceHexes(ctx context.Context, geoID string, params HexesParams) (*HexPage, error) {
	geoID = strings.TrimSpace(geoID)
	if geoID == "" {
		return nil, &ValidationError{
			Parameter: "geo_id",
			Message:   "a geo_id is required; resolve one with /geo/search or /geo",
		}
	}
	if params.Limit > HexesMaxLimit {
		return nil, &ValidationError{
			Parameter: "limit",
			Message:   fmt.Sprintf("/geo/{geo_id}/hexes takes a limit of at most %d", HexesMaxLimit),
		}
	}

	v := url.Values{}
	setString(v, "cursor", params.Cursor)
	setPositive(v, "limit", params.Limit)

	var out HexPage
	path := "/geo/" + url.PathEscape(geoID) + "/hexes"
	meta, err := c.get(ctx, request{path: path, query: v}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// metadata — free
// ─────────────────────────────────────────────────────────────────────────────

// CoverageParams addresses /coverage.
type CoverageParams struct {
	Country string
}

// Coverage lists the markets this API serves, computed live. Free.
//
// ⚠️ prices_available and searchable answer different questions, and the gap
// between them is the useful part: a market can hold listings and still name
// nothing.
func (c *Client) Coverage(ctx context.Context, params CoverageParams) (*CoverageResponse, error) {
	v := url.Values{}
	setString(v, "country", params.Country)

	var out CoverageResponse
	meta, err := c.get(ctx, request{path: string(EndpointCoverage), query: v}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// MetaFilters is every value the filter parameters accept. Free.
func (c *Client) MetaFilters(ctx context.Context) (*FiltersResponse, error) {
	var out FiltersResponse
	meta, err := c.get(ctx, request{path: string(EndpointMetaFilters)}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// Usage is what this token's account has spent and has left. Free.
//
// ⚠️ used and remaining include usage not yet committed to the billing row,
// so these are the numbers to trust right after a burst.
func (c *Client) Usage(ctx context.Context) (*UsageResponse, error) {
	var out UsageResponse
	meta, err := c.get(ctx, request{path: string(EndpointUsage)}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// stats — METERED
// ─────────────────────────────────────────────────────────────────────────────

// StatsParams is the territory and the filters, shared by both /stats routes.
//
// ⚠️ Name the territory with EXACTLY ONE of three selectors. Two is refused
// server-side, and refused here first so it costs no request:
//
//	H3     — one or more cells, all at one resolution
//	GeoID  — a place or an ISO country code
//	Lat+Lng+RadiusKm — a circle
//
// ⚠️ Price filters are ALWAYS USD, whatever Currency displays. Currency is a
// display setting; MinPriceUSD/MaxPriceUSD are the filter.
type StatsParams struct {
	H3       []string
	GeoID    string
	Lat      *float64
	Lng      *float64
	RadiusKm *float64

	// Res applies to the GeoID and point selectors only. Sending it with H3
	// is a contradiction and is refused — a cell states its own resolution.
	Res *int

	AdType    string
	AdSubType string
	Rooms     []string

	MinSize     *float64
	MaxSize     *float64
	MinPriceUSD *float64
	MaxPriceUSD *float64

	// Currency is the DISPLAY currency. It does not change which listings
	// are selected.
	Currency string
}

// selector reports which territory selector was named, after checking that
// exactly one was.
func (p StatsParams) selector() (string, error) {
	var named []string
	if len(p.H3) > 0 {
		named = append(named, "h3")
	}
	if strings.TrimSpace(p.GeoID) != "" {
		named = append(named, "geo_id")
	}
	pointFields := 0
	for _, set := range []bool{p.Lat != nil, p.Lng != nil, p.RadiusKm != nil} {
		if set {
			pointFields++
		}
	}
	if pointFields > 0 {
		named = append(named, "point")
	}

	switch {
	case len(named) == 0:
		return "", &ValidationError{
			Selectors: []string{"h3", "geo_id", "point"},
			Message: "name a territory with exactly one of --h3, --geo-id, or --lat/--lng/--radius-km; " +
				"resolve a geo_id with `investviews geo search` or `investviews geo browse`",
		}
	case len(named) > 1:
		return "", &ValidationError{
			Parameter: named[0],
			Selectors: named,
			Message: fmt.Sprintf("a request names ONE territory. Got %s — drop all but one. "+
				"Two selectors have two different answers and no way to know which you meant",
				strings.Join(named, " and ")),
		}
	case named[0] == "point" && pointFields != 3:
		return "", &ValidationError{
			Selectors: []string{"lat", "lng", "radius_km"},
			Message:   "the circle selector needs all three of lat, lng and radius_km",
		}
	}

	// ⚠️ CELLS ARE CHECKED BEFORE THEY ARE SPENT, and this is the backstop
	// every caller passes through — the CLI checks earlier so it can name
	// the flag, but a cell list reaching here unchecked would go out on a
	// METERED endpoint. See h3.go for why a digits-only test is not enough.
	//
	// It runs AFTER the one-territory guard on purpose: a request naming two
	// selectors is told to drop one, not lectured about the contents of the
	// one it is being told to drop.
	if named[0] == "h3" {
		if err := ValidateH3Cells(p.H3); err != nil {
			return "", err
		}
	}

	if p.Res != nil {
		switch named[0] {
		case "h3":
			return "", &ValidationError{
				Parameter: "res",
				Message: "res is refused alongside h3: a cell id encodes its own resolution, " +
					"so a second opinion about it could only disagree",
			}
		case "geo_id":
			return "", &ValidationError{
				Parameter: "res",
				Message: "res is refused alongside geo_id: a place is answered as one row from " +
					"its own boundary, and there is no cell size to choose. Use --h3 to ask about cells",
			}
		}
	}
	return named[0], nil
}

func (p StatsParams) values() url.Values {
	v := url.Values{}
	if len(p.H3) > 0 {
		v.Set("h3", strings.Join(p.H3, ","))
	}
	setString(v, "geo_id", p.GeoID)
	setFloat(v, "lat", p.Lat)
	setFloat(v, "lng", p.Lng)
	setFloat(v, "radius_km", p.RadiusKm)
	setInt(v, "res", p.Res)

	setString(v, "ad_type", p.AdType)
	setString(v, "ad_sub_type", p.AdSubType)
	if len(p.Rooms) > 0 {
		v.Set("rooms", strings.Join(p.Rooms, ","))
	}
	setFloat(v, "min_size", p.MinSize)
	setFloat(v, "max_size", p.MaxSize)
	setFloat(v, "min_price_usd", p.MinPriceUSD)
	setFloat(v, "max_price_usd", p.MaxPriceUSD)
	setString(v, "currency", p.Currency)
	return v
}

// StatsCurrent is the newest BUILT period for a territory. METERED against
// the current group.
//
// ⚠️ The figures describe ONE PERIOD, never today. Read period, granularity,
// window_start and window_end back from the response and cite them.
//
// ⚠️ An empty Stats is a 200 and a real answer — a covered place that holds
// nothing this period, not a 404 and not a zero.
func (c *Client) StatsCurrent(ctx context.Context, params StatsParams) (*CurrentStats, error) {
	if _, err := params.selector(); err != nil {
		return nil, err
	}
	var out CurrentStats
	meta, err := c.get(ctx, request{path: string(EndpointStatsCurrent), query: params.values()}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// HistoryParams is StatsParams plus the period range.
type HistoryParams struct {
	StatsParams
	// From and To are a period (2025-02) or any date inside one. Omitted,
	// they clamp to what exists — and From omitted means the last 12
	// periods, not the whole history.
	From string
	To   string
}

// StatsHistory is the same figures once per period across a range. METERED
// against the history group, a separate budget from current.
//
// ⚠️ POINTS ARE NOT ADDITIVE — see HistoryResponse.
//
// ⚠️ A range longer than the cap (24 periods) is REFUSED with 422, not
// truncated, and you page it yourself.
func (c *Client) StatsHistory(ctx context.Context, params HistoryParams) (*HistoryResponse, error) {
	if _, err := params.selector(); err != nil {
		return nil, err
	}
	v := params.StatsParams.values()
	setString(v, "from", params.From)
	setString(v, "to", params.To)

	var out HistoryResponse
	meta, err := c.get(ctx, request{path: string(EndpointStatsHistory), query: v}, &out)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	return &out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// query helpers
// ─────────────────────────────────────────────────────────────────────────────

func setString(v url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		v.Set(key, value)
	}
}

func setPositive(v url.Values, key string, value int) {
	if value > 0 {
		v.Set(key, strconv.Itoa(value))
	}
}

func setInt(v url.Values, key string, value *int) {
	if value != nil {
		v.Set(key, strconv.Itoa(*value))
	}
}

// setFloat formats without a trailing .0, so radius_km=5 goes on the wire as
// "5" rather than "5.000000".
func setFloat(v url.Values, key string, value *float64) {
	if value != nil {
		v.Set(key, strconv.FormatFloat(*value, 'f', -1, 64))
	}
}
