package app

// token_usage_test.go covers the token-usage dashboard: the empty state, a
// populated dashboard (totals / series / breakdowns / top sessions / heatmap /
// streak), the bucket switch, and the sync action.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

const usageFacetsFixture = `{"folders":[],"agents":[],"models":[],"data_start":null,"data_end":null}`

// usageFacetsRichFixture fills all three dimensions, so the multi-select
// popovers have something to offer.
const usageFacetsRichFixture = `{
 "folders":[{"folder_id":1,"label":"alpha","name":"alpha","path":"/tmp/alpha"},{"folder_id":2,"label":"beta","name":"beta","path":"/tmp/beta"}],
 "agents":["claude-code","codex"],"models":["claude-opus","gpt-5"],
 "data_start":null,"data_end":null}`

const usageStatusFixture = `{"total_conversations":3,"synced_conversations":2,"stale_conversations":1,"fact_rows":12,"last_synced_at":null,"running":false}`

const usageReportFixture = `{
 "totals":{"input_tokens":1000,"output_tokens":500,"cache_creation_tokens":200,"cache_read_tokens":300,"total_tokens":2000,"turn_count":12,"conversation_count":3,"duration_ms":1000,"active_days":4},
 "previous_totals":null,
 "series":[{"bucket_key":"2024-01-01","total_tokens":1200,"turn_count":6},{"bucket_key":"2024-01-02","total_tokens":800,"turn_count":6}],
 "by_folder":[{"key":"1","label":"proj","total_tokens":1500,"turn_count":9,"conversation_count":2}],
 "by_agent":[{"key":"claude-code","label":"claude-code","total_tokens":2000,"turn_count":12,"conversation_count":3}],
 "by_model":[{"key":"claude-opus","label":"claude-opus","total_tokens":2000,"turn_count":12,"conversation_count":3}],
 "heatmap":[{"weekday":0,"hour":9,"total_tokens":500,"turn_count":2}],
 "top_conversations":[{"conversation_id":7,"title":"Big chat","agent_type":"claude-code","folder_label":"proj","total_tokens":1500,"turn_count":9,"last_activity_at":"2024-01-02T00:00:00Z"}],
 "streak":{"longest_days":5,"current_days":2,"current_ends_on":"2024-01-02"},
 "first_activity_at":"2024-01-01T00:00:00Z","last_activity_at":"2024-01-02T00:00:00Z","truncated":false
}`

const usageEmptyReportFixture = `{"totals":{"total_tokens":0,"turn_count":0,"conversation_count":0,"active_days":0},"series":[],"by_folder":[],"by_agent":[],"by_model":[],"heatmap":[],"top_conversations":[],"streak":{"longest_days":0,"current_days":0},"truncated":false}`

// usageCompareReportFixture has a previous period (1000) so the delta is +100%.
const usageCompareReportFixture = `{
 "totals":{"input_tokens":1000,"output_tokens":500,"cache_creation_tokens":200,"cache_read_tokens":300,"total_tokens":2000,"turn_count":12,"conversation_count":3,"active_days":4},
 "previous_totals":{"input_tokens":500,"output_tokens":250,"cache_creation_tokens":100,"cache_read_tokens":150,"total_tokens":1000,"turn_count":6,"conversation_count":2,"active_days":2},
 "series":[],"by_folder":[],"by_agent":[],"by_model":[],"heatmap":[],"top_conversations":[],
 "streak":{"longest_days":5,"current_days":2},"truncated":false
}`

// usageShellForTest builds a Shell with the usage dashboard active and seeded
// report/facets/status caches so it renders without touching the backend.
func usageShellForTest(t *testing.T, report string) *Shell {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "token-usage"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["usage-facets"] = json.RawMessage(usageFacetsFixture)
	s.cache["usage-status"] = json.RawMessage(usageStatusFixture)
	s.cache[s.usageReportKey("day")] = json.RawMessage(report)
	s.cache[s.usageReportKey("week")] = json.RawMessage(report)
	return s
}

// TestTokenUsageEmpty shows the honest empty state with zero tokens.
func TestTokenUsageEmpty(t *testing.T) {
	s := usageShellForTest(t, usageEmptyReportFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No usage recorded yet.") {
		t.Errorf("empty usage state missing; texts = %v", tt.Texts())
	}
}

// TestTokenUsageRenders shows the totals, series, breakdowns, top session,
// heatmap and streak from a populated report.
func TestTokenUsageRenders(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Token usage",
		"Total tokens",
		"2.0k",              // humanTokens(total)
		"proj",              // by_folder label
		"claude-code",       // by_agent label
		"claude-opus",       // by_model label
		"Big chat",          // top conversation title
		"When you work",     // heatmap heading
		"2 days now",        // streak current
		"5 longest",         // streak longest
		"12 turns",          // status line fact count
		"2/3 conversations", // status line sync ratio
		"Range",             // filter bar
		"Compare previous",  // filter bar
		"Folder",            // filter bar
		"Agent",             // filter bar
		"Model",             // filter bar
	} {
		if !tt.HasText(want) {
			t.Errorf("usage frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestTokenUsageBucketSwitch flips the bucket selector to Week.
func TestTokenUsageBucketSwitch(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Week"); err != nil {
		t.Fatalf("click Week: %v", err)
	}
	tt.Frame()
	if s.usageBucket != "week" {
		t.Errorf("usageBucket = %q, want week", s.usageBucket)
	}
}

// TestTokenUsageSync drives token_usage_sync through the real backend and waits
// for the success line.
func TestTokenUsageSync(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Sync"); err != nil {
		t.Fatalf("click Sync: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("sync complete") }) {
		t.Fatalf("sync result never appeared; texts = %v", tt.Texts())
	}
}

// TestTokenUsageRangeSwitch flips the range preset to 30d.
func TestTokenUsageRangeSwitch(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("30d"); err != nil {
		t.Fatalf("click 30d: %v", err)
	}
	tt.Frame()
	if s.usageRange != "30d" {
		t.Errorf("usageRange = %q, want 30d", s.usageRange)
	}
}

// TestTokenUsageCompare shows the previous-period delta when compare is on.
func TestTokenUsageCompare(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	s.usageCompare = true
	s.cache[s.usageReportKey("day")] = json.RawMessage(usageCompareReportFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("vs previous") {
		t.Fatalf("compare label missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("+100.0%") {
		t.Errorf("delta missing; texts = %v", tt.Texts())
	}
}

// TestTokenUsageMultiSelectFilters drives the three checklist popovers: each
// pick joins the selection, the button names it, and Reset clears them all.
func TestTokenUsageMultiSelectFilters(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	s.cache["usage-facets"] = json.RawMessage(usageFacetsRichFixture)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.Click("Folder"); err != nil {
		t.Fatalf("open Folder filter: %v", err)
	}
	tt.Frame()
	if !tt.HasText("alpha") || !tt.HasText("beta") {
		t.Fatalf("folder checklist missing options; texts = %v", tt.Texts())
	}
	if err := tt.Click("alpha"); err != nil {
		t.Fatalf("check alpha: %v", err)
	}
	tt.Frame()
	if !reflect.DeepEqual(s.usageFolders, []string{"1"}) {
		t.Fatalf("usageFolders = %v, want [1]", s.usageFolders)
	}
	if !tt.HasText("Folder: alpha") {
		t.Errorf("folder button does not name the selection; texts = %v", tt.Texts())
	}

	// The popover stays open across picks: a second folder joins the first.
	if err := tt.Click("beta"); err != nil {
		t.Fatalf("check beta: %v", err)
	}
	tt.Frame()
	if !reflect.DeepEqual(s.usageFolders, []string{"1", "2"}) {
		t.Fatalf("usageFolders = %v, want [1 2]", s.usageFolders)
	}

	if err := tt.Click("Agent"); err != nil {
		t.Fatalf("open Agent filter: %v", err)
	}
	tt.Frame()
	if err := tt.Click("codex"); err != nil {
		t.Fatalf("check codex: %v", err)
	}
	tt.Frame()
	if !reflect.DeepEqual(s.usageAgents, []string{"codex"}) {
		t.Fatalf("usageAgents = %v, want [codex]", s.usageAgents)
	}

	if err := tt.Click("Reset filters"); err != nil {
		t.Fatalf("reset filters: %v", err)
	}
	tt.Frame()
	if s.usageHasFilters() {
		t.Errorf("filters still set after reset: folders=%v agents=%v models=%v",
			s.usageFolders, s.usageAgents, s.usageModels)
	}
	if s.usageFilterOpen != "" {
		t.Errorf("filter popover still open after reset: %q", s.usageFilterOpen)
	}
}

// TestTokenUsageFilterBodyArrays pins the wire shape: each dimension rides as
// an array, and an empty dimension is omitted (the backend reads that as all).
func TestTokenUsageFilterBodyArrays(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	s.usageFolders = []string{"1", "2"}
	s.usageAgents = []string{"claude-code", "codex"}
	s.usageModels = []string{"claude-opus"}

	raw, err := json.Marshal(s.usageFilterBody("day"))
	if err != nil {
		t.Fatalf("marshal filter: %v", err)
	}
	var got struct {
		FolderIDs  []int64  `json:"folderIds"`
		AgentTypes []string `json:"agentTypes"`
		Models     []string `json:"models"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal filter: %v", err)
	}
	if !reflect.DeepEqual(got.FolderIDs, []int64{1, 2}) {
		t.Errorf("folderIds = %v, want [1 2]; body = %s", got.FolderIDs, raw)
	}
	if !reflect.DeepEqual(got.AgentTypes, []string{"claude-code", "codex"}) {
		t.Errorf("agentTypes = %v; body = %s", got.AgentTypes, raw)
	}
	if !reflect.DeepEqual(got.Models, []string{"claude-opus"}) {
		t.Errorf("models = %v; body = %s", got.Models, raw)
	}

	s.resetUsageFilters()
	raw, err = json.Marshal(s.usageFilterBody("day"))
	if err != nil {
		t.Fatalf("marshal empty filter: %v", err)
	}
	var empty map[string]any
	if err := json.Unmarshal(raw, &empty); err != nil {
		t.Fatalf("unmarshal empty filter: %v", err)
	}
	for _, key := range []string{"folderIds", "agentTypes", "models"} {
		if _, ok := empty[key]; ok {
			t.Errorf("empty filter still sends %q; body = %s", key, raw)
		}
	}
}

// TestTokenUsageHeatmapTooltip hovers a heatmap cell and waits for mygo's
// native tooltip, which names the weekday, hour and token count.
func TestTokenUsageHeatmapTooltip(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	// Tall enough that the heatmap is laid out inside the viewport.
	tt := ui.NewTester(s.View, 1240, 1500)
	mon, ok := tt.Find("Mon")
	if !ok || mon.W == 0 || mon.H == 0 {
		t.Fatalf("weekday rail missing; rect = %+v texts = %v", mon, tt.Texts())
	}
	// Row layout: [weekday rail][gap][cell 0][gap]…, so hour 9's center is
	// nine cells along from the rail's end.
	x := mon.X + usageHeatLabelW + usageHeatGap + 9*(usageHeatCellW+usageHeatGap) + usageHeatCellW/2
	y := mon.Y + mon.H/2
	tt.Move(x, y)
	tt.Advance(700 * time.Millisecond) // the tooltip rests for 600ms
	if !tt.HasText("Mon 09:00") {
		t.Fatalf("heatmap tooltip missing at (%v,%v); texts = %v", x, y, tt.Texts())
	}
	if !tt.HasText("500 tokens") {
		t.Errorf("tooltip does not carry the cell value; texts = %v", tt.Texts())
	}
}

// TestUsageFilterSearch narrows an open checklist with its search box
// (upstream's searchable filter) and clears the query with the selection.
func TestUsageFilterSearch(t *testing.T) {
	s := usageShellForTest(t, usageReportFixture)
	s.cache["usage-facets"] = json.RawMessage(usageFacetsRichFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Folder"); err != nil {
		t.Fatalf("open Folder filter: %v", err)
	}
	tt.Frame()
	s.usageFilterQuery = "alp"
	tt.Frame()
	if !tt.HasText("alpha") {
		t.Fatalf("match missing; texts = %v", tt.Texts())
	}
	if tt.HasText("beta") {
		t.Fatalf("non-match stayed; texts = %v", tt.Texts())
	}
	// Picking through the narrowed list still joins the selection.
	if err := tt.Click("alpha"); err != nil {
		t.Fatalf("check alpha: %v", err)
	}
	tt.Frame()
	if !reflect.DeepEqual(s.usageFolders, []string{"1"}) {
		t.Fatalf("usageFolders = %v, want [1]", s.usageFolders)
	}
	// Reopening the filter starts a fresh query. The open state's button is
	// named by the selection, so close through the state and click that name.
	s.usageFilterOpen = ""
	s.usageFilterQuery = ""
	tt.Frame()
	if err := tt.Click("Folder: alpha"); err != nil {
		t.Fatalf("reopen Folder filter: %v", err)
	}
	tt.Frame()
	if s.usageFilterQuery != "" {
		t.Fatalf("query survived reopening: %q", s.usageFilterQuery)
	}
	if !tt.HasText("beta") {
		t.Fatalf("unfiltered list missing beta; texts = %v", tt.Texts())
	}
}
