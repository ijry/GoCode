package app

// token_usage.go — the token-usage dashboard (workbench route "token-usage").
// It renders token_usage_report (totals / series / breakdowns / top sessions /
// heatmap) with a day/week/month bucket selector, plus the sync status line.
//
// Backend contract (handlers/token_usage.go): token_usage_report {filter} →
// {totals, series, by_folder, by_agent, by_model, heatmap, top_conversations,
// streak, truncated}; token_usage_facets; token_usage_status; token_usage_sync.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// viewTokenUsage renders the dashboard.
func (s *Shell) viewTokenUsage(c *ui.Context) {
	t := c.Theme()
	bucket := s.usageBucket
	if bucket == "" {
		bucket = "day"
	}
	if s.usageRange == "" {
		s.usageRange = "all"
	}
	facets := asObj(s.ensure("usage-facets", "token_usage_facets", map[string]any{}))
	status := asObj(s.ensure("usage-status", "token_usage_status", map[string]any{}))
	report := asObj(s.ensure(s.usageReportKey(bucket), "token_usage_report", map[string]any{
		"filter": s.usageFilterBody(bucket),
	}))

	// Header: title, bucket selector, sync.
	ui.Row(c.Key("usage-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).UsageTitle()).FontSize(18).Bold()
		ui.Spacer(c)
		for _, b := range []struct{ id, label string }{
			{"day", cat(c).UsageBucketDay()},
			{"week", cat(c).UsageBucketWeek()},
			{"month", cat(c).UsageBucketMonth()},
		} {
			var clicked bool
			if s.usageBucket == b.id {
				clicked = ui.PrimaryButton(c.Key("usage-b-"+b.id), b.label).Clicked()
			} else {
				clicked = ui.Button(c.Key("usage-b-"+b.id), b.label).Clicked()
			}
			if clicked && s.usageBucket != b.id {
				s.usageBucket = b.id // the filter-aware cache key changes → refetch
			}
		}
		if ui.Button(c.Key("usage-sync"), cat(c).UsageSync()).Clicked() {
			s.syncTokenUsage(c)
		}
	})

	// Filter bar: range presets, folder/agent/model, compare-to-previous.
	s.viewUsageFilters(c, facets)

	totals := asObjVal(report["totals"])
	if totals == nil {
		loadingState(c, "")
		s.viewActionOrError(c)
		return
	}
	if intOf(totals["total_tokens"]) == 0 {
		emptyState(c, iconInfo, cat(c).UsageEmpty(), "")
		s.viewActionOrError(c)
		return
	}

	// Totals row.
	ui.Row(c.Key("usage-totals")).Gap(20).Children(func() {
		usageStat(c, "usage-total", cat(c).UsageTotal(), humanTokens(intOf(totals["total_tokens"])))
		usageStat(c, "usage-in", cat(c).UsageInput(), humanTokens(intOf(totals["input_tokens"])))
		usageStat(c, "usage-out", cat(c).UsageOutput(), humanTokens(intOf(totals["output_tokens"])))
		usageStat(c, "usage-cw", cat(c).UsageCacheCreate(), humanTokens(intOf(totals["cache_creation_tokens"])))
		usageStat(c, "usage-cr", cat(c).UsageCacheRead(), humanTokens(intOf(totals["cache_read_tokens"])))
		usageStat(c, "usage-turns", cat(c).UsageTurns(), fmt.Sprint(intOf(totals["turn_count"])))
		usageStat(c, "usage-convs", cat(c).UsageConversations(), fmt.Sprint(intOf(totals["conversation_count"])))
		usageStat(c, "usage-days", cat(c).UsageActiveDays(), fmt.Sprint(intOf(totals["active_days"])))
	})

	// Streak.
	if streak := asObjVal(report["streak"]); streak != nil {
		cur := intOf(streak["current_days"])
		longest := intOf(streak["longest_days"])
		ui.Text(c, cat(c).UsageStreak()+" · "+cat(c).UsageStreakValue(cur, longest)).FontSize(12).TextColor(t.TextMuted)
	}
	// Compare to the previous equal-length period (needs a bounded range).
	if s.usageCompare {
		if prev := asObjVal(report["previous_totals"]); prev != nil {
			ui.Text(c, cat(c).UsageVsPrevious()+": "+
				usageDelta(intOf(totals["total_tokens"]), intOf(prev["total_tokens"]))).
				FontSize(12).TextColor(t.TextMuted)
		}
	}
	if truthy(report["truncated"]) {
		ui.Text(c, cat(c).UsageTruncated()).FontSize(12).TextColor(t.TextMuted).Wrap()
	}

	ui.Divider(c)

	// Activity bars (most recent ~14 buckets).
	series := asListVal(report["series"])
	if len(series) > 0 {
		ui.Text(c, cat(c).UsageSeries()).Bold()
		maxTok := 1
		for _, pt := range series {
			if v := intOf(pt["total_tokens"]); v > maxTok {
				maxTok = v
			}
		}
		start := 0
		if len(series) > 14 {
			start = len(series) - 14
		}
		ui.Column(c.Key("usage-series")).Gap(3).FillWidth().Children(func() {
			for _, pt := range series[start:] {
				bk := str(pt, "bucket_key")
				usageBar(c, "usage-s-"+bk, bk, intOf(pt["total_tokens"]), maxTok)
			}
		})
	}

	// Breakdowns.
	s.usageBreakdown(c, "folder", cat(c).UsageByFolder(), asListVal(report["by_folder"]))
	s.usageBreakdown(c, "agent", cat(c).UsageByAgent(), asListVal(report["by_agent"]))
	s.usageBreakdown(c, "model", cat(c).UsageByModel(), asListVal(report["by_model"]))

	// Top conversations.
	if tops := asListVal(report["top_conversations"]); len(tops) > 0 {
		ui.Text(c, cat(c).UsageTopConversations()).Bold()
		ui.Column(c.Key("usage-tops")).Gap(4).FillWidth().Children(func() {
			for _, tc := range tops {
				title := str(tc, "title")
				if title == "" {
					title = cat(c).Untitled()
				}
				ui.Row(c.Key("usage-top-" + fmt.Sprint(intOf(tc["conversation_id"])))).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, title).FontSize(12).Grow(1).MaxLines(1)
					ui.Text(c, str(tc, "agent_type")).FontSize(11).TextColor(t.TextMuted)
					ui.Text(c, humanTokens(intOf(tc["total_tokens"]))).FontSize(12).Bold()
				})
			}
		})
	}

	// Heatmap.
	s.usageHeatmap(c, asListVal(report["heatmap"]))

	// Status line.
	fact := intOf(status["fact_rows"])
	synced := intOf(status["synced_conversations"])
	total := intOf(status["total_conversations"])
	ui.Text(c, cat(c).UsageStatus(fact, synced, total)).FontSize(11).TextColor(t.TextMuted)

	s.viewActionOrError(c)
}

// usageStat renders one big-number stat.
func usageStat(c *ui.Context, key, label, value string) {
	t := c.Theme()
	ui.Column(c.Key(key)).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(18).Bold()
	})
}

// usageBar renders a label + proportional bar + value.
func usageBar(c *ui.Context, key, label string, val, max int) {
	t := c.Theme()
	frac := 0.0
	if max > 0 {
		frac = float64(val) / float64(max)
	}
	w := int(300 * frac)
	if w < 2 {
		w = 2
	}
	ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted).Width(120).MaxLines(1)
		ui.Box(c.Key(key + "-bar")).Width(float32(w)).Height(10).Radius(3).Background(t.Text.Alpha(0.30))
		ui.Text(c, humanTokens(val)).FontSize(11)
	})
}

// usageBreakdown renders a top-N breakdown list (folder / agent / model).
func (s *Shell) usageBreakdown(c *ui.Context, kind, title string, items []map[string]any) {
	if len(items) == 0 {
		return
	}
	max := 1
	for _, it := range items {
		if v := intOf(it["total_tokens"]); v > max {
			max = v
		}
	}
	ui.Text(c, title).Bold()
	n := len(items)
	if n > 6 {
		n = 6
	}
	ui.Column(c.Key("usage-bd-" + kind)).Gap(3).FillWidth().Children(func() {
		for i, it := range items[:n] {
			label := str(it, "label")
			if label == "" {
				label = str(it, "key")
			}
			usageBar(c, fmt.Sprintf("usage-%s-%d", kind, i), label, intOf(it["total_tokens"]), max)
		}
	})
}

// Heatmap geometry: weekday rail, then 24 square cells per row. Fixed sizes
// keep the header ruler and the grid aligned.
const (
	usageHeatLabelW = 30
	usageHeatCellW  = 14
	usageHeatCellH  = 14
	usageHeatGap    = 2
)

// usageHeatmap renders the weekday × hour intensity grid (7 rows, 24 columns)
// with a weekday rail, a sparse hour ruler, a legend, and a hover tooltip per
// cell. The tooltip is mygo's native one, so it also reads on keyboard focus
// for assistive technology through the cell's description.
func (s *Shell) usageHeatmap(c *ui.Context, cells []map[string]any) {
	if len(cells) == 0 {
		return
	}
	t := c.Theme()
	weekdays := [7]string{
		cat(c).UsageWeekMon(), cat(c).UsageWeekTue(), cat(c).UsageWeekWed(),
		cat(c).UsageWeekThu(), cat(c).UsageWeekFri(), cat(c).UsageWeekSat(),
		cat(c).UsageWeekSun(),
	}
	var grid [7][24]int
	max := 1
	for _, cell := range cells {
		wd := intOf(cell["weekday"])
		hr := intOf(cell["hour"])
		if wd < 0 || wd > 6 || hr < 0 || hr > 23 {
			continue
		}
		v := intOf(cell["total_tokens"])
		grid[wd][hr] = v
		if v > max {
			max = v
		}
	}
	ui.Text(c, cat(c).UsageHeatmap()).Bold()
	hint := cat(c).UsageHeatmapHint()
	if peak := usagePeakHour(cells); peak >= 0 {
		hint += " " + cat(c).UsageHeatmapPeak(fmt.Sprintf("%02d", peak))
	}
	ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)

	ui.Column(c.Key("usage-heat")).Gap(usageHeatGap).Children(func() {
		ui.Row(c).Gap(usageHeatGap).Children(func() {
			ui.Box(c).Width(usageHeatLabelW)
			for hr := 0; hr < 24; hr++ {
				box := ui.Box(c).Width(usageHeatCellW)
				if hr%6 == 0 {
					box.Children(func() {
						ui.Text(c, fmt.Sprintf("%02d", hr)).FontSize(9).TextColor(t.TextMuted)
					})
				}
			}
		})
		for wd := 0; wd < 7; wd++ {
			ui.Row(c.Key(fmt.Sprintf("usage-heat-%d", wd))).Gap(usageHeatGap).
				AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(usageHeatLabelW).Children(func() {
					ui.Text(c, weekdays[wd]).FontSize(9).TextColor(t.TextMuted)
				})
				for hr := 0; hr < 24; hr++ {
					v := grid[wd][hr]
					alpha := 0.06
					if v > 0 {
						alpha = 0.15 + 0.75*float64(v)/float64(max)
					}
					tip := cat(c).UsageHeatmapCell(weekdays[wd], fmt.Sprintf("%02d", hr), humanTokens(v))
					ui.Box(c.Key(fmt.Sprintf("usage-cell-%d-%d", wd, hr))).
						Width(usageHeatCellW).Height(usageHeatCellH).Radius(2).
						Background(t.Text.Alpha(float32(alpha))).Tooltip(tip)
				}
			})
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Spacer(c)
			ui.Text(c, cat(c).UsageLess()).FontSize(9).TextColor(t.TextMuted)
			for _, a := range []float32{0.06, 0.15, 0.34, 0.52, 0.71, 0.9} {
				ui.Box(c).Width(10).Height(10).Radius(2).Background(t.Text.Alpha(a))
			}
			ui.Text(c, cat(c).UsageMore()).FontSize(9).TextColor(t.TextMuted)
		})
	})
}

// usagePeakHour is the hour with the most tokens across the week, or -1 when
// the heatmap is empty (upstream's peakHour).
func usagePeakHour(cells []map[string]any) int {
	var byHour [24]int
	for _, cell := range cells {
		hr := intOf(cell["hour"])
		if hr < 0 || hr > 23 {
			continue
		}
		byHour[hr] += intOf(cell["total_tokens"])
	}
	best := -1
	for hr, v := range byHour {
		if v <= 0 {
			continue
		}
		if best < 0 || v > byHour[best] {
			best = hr
		}
	}
	return best
}

// syncTokenUsage runs token_usage_sync off the UI thread, then drops every usage
// cache key so the dashboard refetches.
func (s *Shell) syncTokenUsage(c *ui.Context) {
	loc := c.Locale()
	svc := s.svc
	go func() {
		_, err := s.B.raw("token_usage_sync", map[string]any{})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).UsageSynced()
			delete(s.cache, "usage-status")
			delete(s.cache, "usage-facets")
			delete(s.cache, "usage-report:day")
			delete(s.cache, "usage-report:week")
			delete(s.cache, "usage-report:month")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// usageReportKey folds every filter dimension into the cache key, so changing a
// filter fetches a fresh report instead of reusing a stale one.
func (s *Shell) usageReportKey(bucket string) string {
	return fmt.Sprintf("usage-report:%s:%s:%s:%s:%s:%t",
		bucket, s.usageRange,
		strings.Join(s.usageFolders, ","),
		strings.Join(s.usageAgents, ","),
		strings.Join(s.usageModels, ","),
		s.usageCompare)
}

// usageRangeBounds returns the [start,end] RFC3339 bounds for a range preset
// ("" for "all").
func usageRangeBounds(r string) (string, string) {
	days := 0
	switch r {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	}
	if days == 0 {
		return "", ""
	}
	now := time.Now().UTC()
	return now.AddDate(0, 0, -days).Format(time.RFC3339), now.Format(time.RFC3339)
}

// usageFilterBody builds the token_usage_report filter from the current state.
// The three dimensions are arrays on the wire; an empty selection leaves the
// key out, which the backend reads as "all".
func (s *Shell) usageFilterBody(bucket string) map[string]any {
	f := map[string]any{
		"bucket":          bucket,
		"tzOffsetMinutes": tzOffsetMinutes(),
		"comparePrevious": s.usageCompare,
	}
	if start, end := usageRangeBounds(s.usageRange); start != "" {
		f["start"] = start
		f["end"] = end
	}
	if ids := usageFolderIDs(s.usageFolders); len(ids) > 0 {
		f["folderIds"] = ids
	}
	if len(s.usageAgents) > 0 {
		f["agentTypes"] = append([]string(nil), s.usageAgents...)
	}
	if len(s.usageModels) > 0 {
		f["models"] = append([]string(nil), s.usageModels...)
	}
	return f
}

// usageFolderIDs turns the selected folder ids (kept as strings for the
// checklist) back into the numbers the report expects.
func usageFolderIDs(selected []string) []int64 {
	ids := make([]int64, 0, len(selected))
	for _, v := range selected {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// usageHasFilters reports whether any dimension narrows the report.
func (s *Shell) usageHasFilters() bool {
	return len(s.usageFolders)+len(s.usageAgents)+len(s.usageModels) > 0
}

// resetUsageFilters clears every dimension, matching upstream's Reset filters.
func (s *Shell) resetUsageFilters() {
	s.usageFolders, s.usageAgents, s.usageModels = nil, nil, nil
	s.usageFilterOpen = ""
}

// usageMultiOption is one row of a dimension filter's checklist. The value is
// what the report takes (a folder id, agent type or model); the label is what
// the checklist shows.
type usageMultiOption struct {
	value string
	label string
}

// viewUsageFilters renders the range presets, the three multi-select dimension
// filters and the compare toggle. Upstream's MultiSelectFilter is a pill that
// opens a checklist, so each dimension here is a button anchoring a ui.Popover
// of native checkboxes — no custom widget.
func (s *Shell) viewUsageFilters(c *ui.Context, facets map[string]any) {
	t := c.Theme()
	ui.Row(c.Key("usage-range")).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).UsageRange()).FontSize(11).TextColor(t.TextMuted)
		for _, r := range []struct{ id, label string }{
			{"all", cat(c).UsageAll()},
			{"7d", cat(c).Usage7d()},
			{"30d", cat(c).Usage30d()},
			{"90d", cat(c).Usage90d()},
		} {
			var clicked bool
			if s.usageRange == r.id {
				clicked = ui.PrimaryButton(c.Key("usage-r-"+r.id), r.label).Clicked()
			} else {
				clicked = ui.Button(c.Key("usage-r-"+r.id), r.label).Clicked()
			}
			if clicked {
				s.usageRange = r.id
			}
		}
	})

	ui.Row(c.Key("usage-dims")).Gap(10).AlignItems(ui.Center).Children(func() {
		folderOpts := []usageMultiOption{}
		for _, f := range asListVal(facets["folders"]) {
			if label := str(f, "label"); label != "" {
				folderOpts = append(folderOpts, usageMultiOption{
					value: fmt.Sprint(intOf(f["folder_id"])),
					label: label,
				})
			}
		}
		agentOpts := make([]usageMultiOption, 0)
		for _, a := range asStringList(facets["agents"]) {
			agentOpts = append(agentOpts, usageMultiOption{value: a, label: a})
		}
		modelOpts := make([]usageMultiOption, 0)
		for _, m := range asStringList(facets["models"]) {
			modelOpts = append(modelOpts, usageMultiOption{value: m, label: m})
		}
		s.usageMultiFilter(c, "folder", cat(c).UsageFolderFilter(), folderOpts, &s.usageFolders)
		s.usageMultiFilter(c, "agent", cat(c).UsageAgentFilter(), agentOpts, &s.usageAgents)
		s.usageMultiFilter(c, "model", cat(c).UsageModelFilter(), modelOpts, &s.usageModels)
		if s.usageHasFilters() {
			if ui.Button(c.Key("usage-reset"), cat(c).UsageResetFilters()).Clicked() {
				s.resetUsageFilters()
			}
		}

		if s.usageCompare {
			if ui.PrimaryButton(c.Key("usage-cmp"), cat(c).UsageComparePrev()).Clicked() {
				s.usageCompare = false
			}
		} else {
			if ui.Button(c.Key("usage-cmp"), cat(c).UsageComparePrev()).Clicked() {
				s.usageCompare = true
			}
		}
	})
}

// usageMultiFilter is one dimension's button + checklist popover. The button
// names the selection ("Folder: proj +1") the way upstream's pill does; the
// popover stays open across checks so several values can be picked at once.
func (s *Shell) usageMultiFilter(c *ui.Context, kind, label string, options []usageMultiOption, sel *[]string) {
	t := c.Theme()
	name := label
	if len(*sel) > 0 {
		name = label + ": " + usageSelectionSummary(options, *sel)
	}
	open := s.usageFilterOpen == kind
	btn := ui.Button(c.Key("usage-filter-"+kind), name)
	if btn.Clicked() {
		open = !open
		if open {
			s.usageFilterOpen = kind
		} else if s.usageFilterOpen == kind {
			s.usageFilterOpen = ""
		}
	}
	ui.Popover(c, btn, &open, func() {
		ui.Column(c.Key("usage-pop-" + kind)).Width(220).Padding(10).Gap(6).Children(func() {
			if len(options) == 0 {
				ui.Text(c, cat(c).UsageNoMatches()).FontSize(12).TextColor(t.TextMuted)
				return
			}
			for i, opt := range options {
				on := containsString(*sel, opt.value)
				cb := ui.Checkbox(c.Key(fmt.Sprintf("usage-opt-%s-%d", kind, i)), &on, opt.label)
				if cb.Changed() {
					*sel = toggleString(*sel, opt.value, on)
				}
			}
		})
	})
	// Popover turns its own copy of open off on Escape or an outside press;
	// mirror that so the next frame does not reopen it.
	if !open && s.usageFilterOpen == kind {
		s.usageFilterOpen = ""
	}
}

// usageSelectionSummary names a multi-value selection: the first value's label,
// then how many more are picked.
func usageSelectionSummary(options []usageMultiOption, sel []string) string {
	if len(sel) == 0 {
		return ""
	}
	first := sel[0]
	for _, opt := range options {
		if opt.value == first {
			first = opt.label
			break
		}
	}
	if len(sel) == 1 {
		return first
	}
	return fmt.Sprintf("%s +%d", first, len(sel)-1)
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func toggleString(list []string, v string, on bool) []string {
	if on {
		if containsString(list, v) {
			return list
		}
		return append(list, v)
	}
	out := list[:0]
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

// usageDelta renders the percentage change from prev to cur.
func usageDelta(cur, prev int) string {
	if prev == 0 {
		return "—"
	}
	pct := (float64(cur) - float64(prev)) / float64(prev) * 100
	if pct >= 0 {
		return fmt.Sprintf("+%.1f%%", pct)
	}
	return fmt.Sprintf("%.1f%%", pct)
}

// humanTokens compacts a token count (1.2k / 3.4M / 5.6B).
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// tzOffsetMinutes is the viewer's current UTC offset in minutes (the backend
// buckets by this).
func tzOffsetMinutes() int {
	_, off := time.Now().Zone()
	return off / 60
}
