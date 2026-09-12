package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/investviews/investviews-cli/internal/api"
)

// NewStatsCmd builds "investviews stats": the two METERED commands.
func NewStatsCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Market figures for a territory (METERED)",
		Long: `Market figures for a territory.

⚠️ THESE ARE THE ONLY TWO COMMANDS THAT SPEND QUOTA. Everything under "geo",
plus "coverage" and "usage", is free and uncounted, so resolve the place first
and spend a stats call once you know it holds data. Each run prints what it
cost, taken from the quota headers the server sent back.

"current" and "history" bill against SEPARATE budgets.`,
	}
	cmd.AddCommand(newStatsCurrentCmd(deps), newStatsHistoryCmd(deps))
	return cmd
}

// selectorFlags is the territory and the filters, shared by both stats
// commands so that one selector rule is written once.
type selectorFlags struct {
	geoID    string
	h3       []string
	lat      float64
	lng      float64
	radiusKm float64
	res      int

	adType    string
	adSubType string
	rooms     []string

	minSize     float64
	maxSize     float64
	minPriceUSD float64
	maxPriceUSD float64
	currency    string
}

func (s *selectorFlags) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&s.geoID, "geo-id", "",
		"a place ref (R344953) or a two-letter country code (es) — one parameter, both kinds")
	f.StringArrayVar(&s.h3, "h3", nil,
		"H3 cells: repeat the flag, comma-join them, or pass - to read them from standard input. "+
			"Ids are DECIMAL strings (613498076398616575), not 87… hex")
	f.Float64Var(&s.lat, "lat", 0, "circle centre latitude; requires --lng and --radius-km")
	f.Float64Var(&s.lng, "lng", 0, "circle centre longitude; requires --lat and --radius-km")
	f.Float64Var(&s.radiusKm, "radius-km", 0, "circle radius in kilometres, at most 500 (alias: --radius)")
	f.IntVar(&s.res, "res", 0, "H3 resolution 4..8; refused alongside --h3, whose cells state their own")

	f.StringVar(&s.adType, "ad-type", "",
		"asset class, e.g. real_estate_residential; see `investviews coverage --help` and /meta/filters")
	f.StringVar(&s.adSubType, "ad-sub-type", "", "buy (for sale) or rent (to let); the API defaults to buy")
	f.StringArrayVar(&s.rooms, "rooms", nil,
		"room bins: 1, 2, 3, 4, 5+, unknown. Repeatable or comma-joined. 'unknown' is a REAL bin")
	f.Float64Var(&s.minSize, "min-size", 0, "minimum living area, m²")
	f.Float64Var(&s.maxSize, "max-size", 0, "maximum living area, m²")
	f.Float64Var(&s.minPriceUSD, "min-price-usd", 0, "minimum price in USD — always USD, whatever --currency displays")
	f.Float64Var(&s.maxPriceUSD, "max-price-usd", 0, "maximum price in USD — always USD, whatever --currency displays")
	f.StringVar(&s.currency, "currency", "", "DISPLAY currency for the figures; it does not change which listings are selected")

	// --radius is accepted as an alias for --radius-km. The API's parameter
	// is radius_km and the unit belongs in the name, but --radius is the
	// short form people type; an alias is cheaper than a wrong unit.
	f.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "radius" {
			return pflag.NormalizedName("radius-km")
		}
		return pflag.NormalizedName(name)
	})
}

// params turns the flags into a request, refusing locally anything the API
// would refuse — so a mistake costs no request and no quota.
func (s *selectorFlags) params(cmd *cobra.Command, in io.Reader) (api.StatsParams, error) {
	cells, err := hexIDs(s.h3, in)
	if err != nil {
		return api.StatsParams{}, err
	}

	f := cmd.Flags()
	hasPoint := f.Changed("lat") || f.Changed("lng") || f.Changed("radius-km")

	// ⚠️ ONE TERRITORY PER REQUEST. The API enforces this too; catching it
	// here means the mistake is free. Two selectors have two different
	// answers and nothing can tell which one was meant.
	var named []string
	if strings.TrimSpace(s.geoID) != "" {
		named = append(named, "--geo-id")
	}
	if len(cells) > 0 {
		named = append(named, "--h3")
	}
	if hasPoint {
		named = append(named, "--lat/--lng/--radius-km")
	}
	switch {
	case len(named) == 0:
		return api.StatsParams{}, &api.ValidationError{
			Selectors: []string{"geo_id", "h3", "point"},
			Message: "name a territory with exactly one of --geo-id, --h3, or --lat/--lng/--radius-km. " +
				"Resolve a geo_id with `investviews geo search` or `investviews geo browse` — both are free",
		}
	case len(named) > 1:
		return api.StatsParams{}, &api.ValidationError{
			Parameter: strings.TrimPrefix(named[0], "--"),
			Selectors: named,
			Message: fmt.Sprintf("a request names ONE territory. Got %s — drop all but one. "+
				"Nothing was sent, so this cost no quota", strings.Join(named, " and ")),
		}
	case hasPoint && !(f.Changed("lat") && f.Changed("lng") && f.Changed("radius-km")):
		return api.StatsParams{}, &api.ValidationError{
			Selectors: []string{"lat", "lng", "radius_km"},
			Message:   "the circle selector needs all three of --lat, --lng and --radius-km",
		}
	}

	params := api.StatsParams{
		GeoID:     strings.TrimSpace(s.geoID),
		H3:        cells,
		AdType:    s.adType,
		AdSubType: s.adSubType,
		Rooms:     splitList(s.rooms),
		Currency:  s.currency,
	}
	if hasPoint {
		params.Lat, params.Lng, params.RadiusKm = &s.lat, &s.lng, &s.radiusKm
	}
	if f.Changed("res") {
		params.Res = &s.res
	}
	if f.Changed("min-size") {
		params.MinSize = &s.minSize
	}
	if f.Changed("max-size") {
		params.MaxSize = &s.maxSize
	}
	if f.Changed("min-price-usd") {
		params.MinPriceUSD = &s.minPriceUSD
	}
	if f.Changed("max-price-usd") {
		params.MaxPriceUSD = &s.maxPriceUSD
	}
	return params, nil
}

// hexIDs expands the --h3 flag: repeats, comma lists, and "-" for standard
// input, which is how a `geo hexes` listing is piped straight in.
func hexIDs(values []string, in io.Reader) ([]string, error) {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) == "-" {
			piped, err := readList(in)
			if err != nil {
				return nil, err
			}
			out = append(out, piped...)
			continue
		}
		out = append(out, splitList([]string{value})...)
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// stats current
// ─────────────────────────────────────────────────────────────────────────────

func newStatsCurrentCmd(deps Deps) *cobra.Command {
	var sel selectorFlags

	cmd := &cobra.Command{
		Use:   "current",
		Short: "Figures for the newest built period (METERED)",
		Long: `Market figures for a territory, from the newest BUILT period.

  investviews stats current --geo-id R344953
  investviews stats current --geo-id es --ad-type real_estate_commercial
  investviews geo hexes R344953 | investviews stats current --h3 -

⚠️ METERED, against the "current" group. Resolve the place first with the free
geo commands, and check its availability line before spending this.

⚠️ THE FIGURES DESCRIBE ONE PAST PERIOD, NEVER TODAY. The period, its window
and the as_of date are printed with every answer; cite them.

⚠️ Price filters are ALWAYS USD (--min-price-usd/--max-price-usd) whatever
--currency displays. --currency changes the display only; it does not change
which listings are selected.

⚠️ An empty answer is a 200 and a NORMAL answer: a covered place that held
nothing in this window. It is not a zero and not an error.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}
	v := addJSONFlag(cmd)
	sel.register(cmd)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := deps.client()
		if err != nil {
			return err
		}
		params, err := sel.params(cmd, cmd.InOrStdin())
		if err != nil {
			return err
		}
		resp, err := client.StatsCurrent(cmd.Context(), params)
		if err != nil {
			return err
		}
		return v.emit(cmd, resp, resp.Meta, 1, api.EndpointStatsCurrent, func(w io.Writer) {
			renderCurrent(w, resp)
		})
	}
	return cmd
}

func renderCurrent(w io.Writer, resp *api.CurrentStats) {
	fmt.Fprintln(w, territoryLine(resp.Territory))
	fmt.Fprintf(w, "period %s (%s) · window %s → %s · as_of %s · fx %s · figures in %s\n",
		str(resp.Period), dash(resp.Granularity), str(resp.WindowStart), str(resp.WindowEnd),
		str(resp.AsOf), str(resp.FxDate), dash(resp.Currency))
	fmt.Fprintln(w, filtersLine(resp.Filters))
	fmt.Fprintln(w)

	if len(resp.Stats) == 0 {
		renderEmptyStats(w, resp)
		return
	}
	renderStatRows(w, resp.Stats, resp.Currency)
	renderStatFootnotes(w, resp.Stats)
}

func renderEmptyStats(w io.Writer, resp *api.CurrentStats) {
	fmt.Fprintf(w, "No figures for this territory in period %s. That is a NORMAL answer, not an\n", str(resp.Period))
	fmt.Fprintln(w, "error and not a zero: the territory is covered, and it held nothing that met the")
	fmt.Fprintln(w, "display floor in this window.")
	if resp.Snapshot.EarliestPeriod != "" {
		fmt.Fprintf(w, "History goes back to %s.\n", resp.Snapshot.EarliestPeriod)
	}
	next := "investviews stats history"
	if resp.Territory.GeoID != "" {
		next += " --geo-id " + resp.Territory.GeoID
	}
	fmt.Fprintf(w, "Next: %s, or a coarser --res, or a wider filter set.\n", next)
}

// renderStatRows prints one row per cell or place. Each row's identity comes
// first, so it can be spent on the next call.
func renderStatRows(w io.Writer, stats []api.Stat, currency string) {
	fmt.Fprintf(w, "%d row(s).\n\n", len(stats))
	table(w, func(tw io.Writer) {
		fmt.Fprintf(tw, "ID\tNAME\tLEVEL\tMEDIAN %s/m²\tMEDIAN PRICE\tMEDIAN m²\tP05/m²\tP95/m²\tNOTES\n",
			dash(currency))
		for _, row := range stats {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				dash(row.H3), str(row.Name), str(row.ZoneLevel),
				num(row.MedianPricePerSqm), num(row.MedianPrice), num(row.MedianArea),
				num(row.P05PricePerSqm), num(row.P95PricePerSqm),
				statNotes(row))
		}
	})
}

// statNotes renders the two quality flags.
//
// ⚠️ low_confidence ABSENT IS NOT FALSE — the API stamps it only where a
// second period read is cheap, so an unstamped row says nothing either way and
// must not be rendered as "confident".
func statNotes(row api.Stat) string {
	var notes []string
	if row.Estimated {
		notes = append(notes, "estimated")
	}
	if row.LowConfidence != nil && *row.LowConfidence {
		notes = append(notes, "low-confidence")
	}
	if len(notes) == 0 {
		return "—"
	}
	return strings.Join(notes, ", ")
}

func renderStatFootnotes(w io.Writer, stats []api.Stat) {
	var estimated, stamped bool
	for _, row := range stats {
		estimated = estimated || row.Estimated
		stamped = stamped || row.LowConfidence != nil
	}
	if estimated {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "⚠️ \"estimated\" rows are NOT that cell's own figures: they are pooled from the")
		fmt.Fprintln(w, "cell and its six neighbours (~1.2 km). Quote them for the area, and never")
		fmt.Fprintln(w, "compare an estimated row against an exact one as if they measured the same thing.")
	}
	if !stamped {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "note: no row carries a low_confidence stamp. The API stamps it only where a")
		fmt.Fprintln(w, "second period read is cheap, so absent means UNSTAMPED — not \"confident\".")
	}
}

func territoryLine(t api.Territory) string {
	var b strings.Builder
	switch {
	case t.Place != nil:
		fmt.Fprintf(&b, "%s (%s, %s)", t.Place.Name, t.Place.Level, t.Place.Country)
		if t.GeoID != "" {
			fmt.Fprintf(&b, " — geo_id %s", t.GeoID)
		}
	case t.GeoID != "":
		fmt.Fprintf(&b, "geo_id %s", t.GeoID)
	case t.Center != nil:
		fmt.Fprintf(&b, "circle at %g, %g", t.Center.Lat, t.Center.Lng)
		if t.RadiusKm != nil {
			fmt.Fprintf(&b, " radius %g km", *t.RadiusKm)
		}
	default:
		fmt.Fprintf(&b, "%d cell(s)", t.HexCount)
	}
	fmt.Fprintf(&b, " · selector %s", dash(t.Selector))
	if t.Resolution != nil {
		fmt.Fprintf(&b, " · res %d", *t.Resolution)
	}
	if t.Grain != "" {
		fmt.Fprintf(&b, " · grain %s", t.Grain)
	}
	if t.HexCount > 0 && t.Selector != "" {
		fmt.Fprintf(&b, " · %d cell(s)", t.HexCount)
	}
	return b.String()
}

// filtersLine echoes the filter set AS APPLIED — defaults included, because a
// filter you did not name still has a value.
func filtersLine(f api.Filters) string {
	var b strings.Builder
	fmt.Fprintf(&b, "filters: %s / %s", dash(f.AdType), dash(f.AdSubType))
	if len(f.Rooms) > 0 {
		fmt.Fprintf(&b, " · rooms %s", strings.Join(f.Rooms, ","))
	}
	if f.MinSize != nil || f.MaxSize != nil {
		fmt.Fprintf(&b, " · size %s..%s m²", num(f.MinSize), num(f.MaxSize))
	}
	if f.MinPriceUSD != nil || f.MaxPriceUSD != nil {
		fmt.Fprintf(&b, " · price %s..%s USD", num(f.MinPriceUSD), num(f.MaxPriceUSD))
	}
	b.WriteString(" · price filters are USD whatever the display currency is")
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// stats history
// ─────────────────────────────────────────────────────────────────────────────

func newStatsHistoryCmd(deps Deps) *cobra.Command {
	var (
		sel      selectorFlags
		from, to string
	)

	cmd := &cobra.Command{
		Use:   "history",
		Short: "The same figures, period by period (METERED)",
		Long: `The same figures as "stats current", once per period across a range.

  investviews stats history --geo-id R344953 --from 2025-02 --to 2025-12

⚠️ METERED, against the "history" group — a SEPARATE budget from "current".

⚠️ POINTS ARE NOT ADDITIVE. Each point is an independent aggregate over its own
window, and at the rolling_3m granularity two adjacent windows overlap by two
months outright. Compare points and draw a line; never sum counts across
periods, and never read a sum as inventory.

--from omitted gives the LAST 12 PERIODS, not the whole history. A range longer
than 24 periods is refused, not truncated — page it yourself.

There is no --interval: the API derives the granularity (month or rolling_3m)
from the resolution and echoes it on every point.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}
	v := addJSONFlag(cmd)
	sel.register(cmd)
	cmd.Flags().StringVar(&from, "from", "", "earliest period, YYYY-MM or any date inside it; omitted, the last 12 periods")
	cmd.Flags().StringVar(&to, "to", "", "latest period, YYYY-MM or any date inside it; omitted, the newest built period")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := deps.client()
		if err != nil {
			return err
		}
		params, err := sel.params(cmd, cmd.InOrStdin())
		if err != nil {
			return err
		}
		resp, err := client.StatsHistory(cmd.Context(), api.HistoryParams{
			StatsParams: params, From: from, To: to,
		})
		if err != nil {
			return err
		}
		return v.emit(cmd, resp, resp.Meta, 1, api.EndpointStatsHistory, func(w io.Writer) {
			renderHistory(w, resp)
		})
	}
	return cmd
}

func renderHistory(w io.Writer, resp *api.HistoryResponse) {
	fmt.Fprintln(w, territoryLine(resp.Territory))
	fmt.Fprintf(w, "%d period(s) · figures in %s\n", len(resp.Series), dash(resp.Currency))
	fmt.Fprintln(w, filtersLine(resp.Filters))
	fmt.Fprintln(w)

	if len(resp.Series) == 0 {
		fmt.Fprintln(w, "No periods in this range. That is a NORMAL answer, not an error.")
		if resp.Snapshot.EarliestPeriod != "" {
			fmt.Fprintf(w, "History goes back to %s — widen --from, or drop it for the last 12 periods.\n",
				resp.Snapshot.EarliestPeriod)
		}
		return
	}

	// One row per period is only honest while each period holds one row. A
	// multi-cell series has no single number per period, and averaging the
	// cells here would invent one.
	multi := false
	for _, point := range resp.Series {
		if len(point.Stats) > 1 {
			multi = true
			break
		}
	}

	table(w, func(tw io.Writer) {
		if multi {
			fmt.Fprintln(tw, "PERIOD\tGRANULARITY\tWINDOW\tAS_OF\tROWS")
			for _, point := range resp.Series {
				fmt.Fprintf(tw, "%s\t%s\t%s → %s\t%s\t%d\n",
					point.Period, point.Granularity, point.WindowStart, point.WindowEnd,
					point.AsOf, len(point.Stats))
			}
			return
		}
		fmt.Fprintf(tw, "PERIOD\tGRANULARITY\tWINDOW\tAS_OF\tMEDIAN %s/m²\tMEDIAN PRICE\tMEDIAN m²\n",
			dash(resp.Currency))
		for _, point := range resp.Series {
			var row api.Stat
			if len(point.Stats) == 1 {
				row = point.Stats[0]
			}
			fmt.Fprintf(tw, "%s\t%s\t%s → %s\t%s\t%s\t%s\t%s\n",
				point.Period, point.Granularity, point.WindowStart, point.WindowEnd, point.AsOf,
				num(row.MedianPricePerSqm), num(row.MedianPrice), num(row.MedianArea))
		}
	})

	if multi {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "A period here holds several cells, so there is no single figure per period.")
		fmt.Fprintln(w, "Use --json for the per-cell figures, or ask about one cell at a time.")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "⚠️ POINTS ARE NOT ADDITIVE — compare them, never sum them.")
	if note := strings.TrimSpace(resp.SeriesNote); note != "" {
		fmt.Fprintln(w, note)
	}
}
