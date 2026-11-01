package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The examples are real output from PostgreSQL 17 on made-up tables.
func load(t *testing.T, name string) *Plan {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(string(data))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return parsed
}

func ids(review Review) map[string]string {
	found := map[string]string{}
	for _, finding := range review.Findings {
		found[finding.ID] = finding.Severity
	}
	return found
}

func TestRealPlansInBothFormats(t *testing.T) {
	cases := map[string][]string{
		"seq-scan-filter":   {"PG101"},
		"misestimate":       {"PG101", "PG105"},
		"sort-disk":         {"PG107"},
		"hash-spill":        {"PG108"},
		"nested-loop":       {"PG106", "PG110"},
		"index-then-filter": {"PG102"},
		"lossy-bitmap":      {"PG111"},
		"no-workers":        {"PG109"},
	}
	for name, want := range cases {
		for _, suffix := range []string{".json", ".txt"} {
			t.Run(name+suffix, func(t *testing.T) {
				review := Analyze(load(t, name+suffix))
				found := ids(review)
				for _, id := range want {
					if _, ok := found[id]; !ok {
						t.Errorf("missing %s, got %v", id, found)
					}
				}
				if review.ExecutionMS <= 0 || !review.Analyzed || len(review.Hottest) == 0 {
					t.Errorf("incomplete: %+v", review)
				}
			})
		}
	}
}

func TestBothFormatsReadTheSameTree(t *testing.T) {
	for _, name := range []string{"sort-disk", "nested-loop", "hash-spill", "lossy-bitmap"} {
		a, b := load(t, name+".json").Root, load(t, name+".txt").Root
		if count(a) != count(b) {
			t.Errorf("%s: %d nodes in JSON, %d in text", name, count(a), count(b))
		}
	}
}

func count(node *Node) int {
	total := 1
	for _, child := range node.Children {
		total += count(child)
	}
	return total
}

func TestIndexHintNamesTableAndColumn(t *testing.T) {
	review := Analyze(load(t, "seq-scan-filter.json"))
	for _, finding := range review.Findings {
		if finding.ID == "PG101" {
			if !strings.Contains(finding.Fix, "CREATE INDEX CONCURRENTLY ON orders (customer_id)") {
				t.Fatal(finding.Fix)
			}
			return
		}
	}
	t.Fatal("no PG101")
}

func TestCompareRealIndexChange(t *testing.T) {
	result := Compare(load(t, "seq-scan-filter.json"), load(t, "seq-scan-fixed.json"))
	if result.ChangePercent >= -50 {
		t.Fatalf("change %v", result.ChangePercent)
	}
	found := false
	for _, finding := range result.Fixed {
		found = found || finding.ID == "PG101"
	}
	if !found || len(result.New) != 0 {
		t.Fatalf("%+v", result)
	}
}

func TestEstimatesOnly(t *testing.T) {
	review := Analyze(load(t, "estimate-only.json"))
	if review.Analyzed || len(review.Findings) != 0 || !strings.Contains(review.Notes[0], "really runs the query") {
		t.Fatalf("%+v", review)
	}
	result := Compare(load(t, "estimate-only.json"), load(t, "seq-scan-fixed.json"))
	if len(result.Notes) != 1 || result.ChangePercent != 0 {
		t.Fatalf("%+v", result)
	}
}

func TestParallelTimeIsWallClock(t *testing.T) {
	// One worker and the leader each took 10 ms, so that's about 10 ms of waiting, not 20.
	input := `Gather  (cost=1.00..2.00 rows=1 width=4) (actual time=0.1..11.000 rows=1 loops=1)
  Workers Planned: 1
  Workers Launched: 1
  ->  Parallel Seq Scan on t  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..10.000 rows=1 loops=2)
Execution Time: 11.000 ms
`
	parsed, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	review := Analyze(parsed)
	if review.Hottest[0].Node != "Seq Scan on t" || review.Hottest[0].SelfMS != 10 {
		t.Fatalf("%+v", review.Hottest)
	}
}

func TestNestedLoopRepeatsAddUp(t *testing.T) {
	input := `Nested Loop  (cost=1.00..2.00 rows=1 width=4) (actual time=0.1..30.000 rows=1 loops=1)
  ->  Seq Scan on a  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..1.000 rows=1 loops=1)
  ->  Index Scan using b_pkey on b  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..0.010 rows=1 loops=2000)
Execution Time: 30.000 ms
`
	parsed, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	review := Analyze(parsed)
	if ids(review)["PG106"] != High {
		t.Fatalf("%+v", review.Findings)
	}
	if review.Hottest[0].Node != "Index Scan using b_pkey on b" || review.Hottest[0].SelfMS != 20 {
		t.Fatalf("%+v", review.Hottest)
	}
}

func TestSmallRowCountsArentFindings(t *testing.T) {
	input := `Seq Scan on t  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..20.000 rows=1 loops=1)
  Filter: (a = 1)
  Rows Removed by Filter: 50
Execution Time: 20.000 ms
`
	parsed, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if review := Analyze(parsed); len(review.Findings) != 0 {
		t.Fatalf("%+v", review.Findings)
	}
}

func TestSeverityFollowsShareOfTime(t *testing.T) {
	cases := []struct {
		impact, total float64
		want          string
	}{{50, 100, High}, {10, 100, Medium}, {2, 100, Low}, {0.5, 1, Low}}
	for _, test := range cases {
		if got := severity(test.impact, test.total); got != test.want {
			t.Errorf("%v of %v: %s", test.impact, test.total, got)
		}
	}
}

func TestJoinFilterThrowingRowsAway(t *testing.T) {
	input := `Nested Loop  (cost=1.00..2.00 rows=1 width=4) (actual time=0.1..20.000 rows=10 loops=1)
  Join Filter: (a.x < b.y)
  Rows Removed by Join Filter: 5000
  ->  Seq Scan on a  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..1.000 rows=10 loops=1)
  ->  Seq Scan on b  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..1.000 rows=500 loops=10)
Execution Time: 20.000 ms
`
	parsed, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids(Analyze(parsed))["PG103"]; !ok {
		t.Fatalf("%+v", Analyze(parsed).Findings)
	}
}

func TestPlanningAndJIT(t *testing.T) {
	input := `Seq Scan on t  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..1.000 rows=1 loops=1)
Planning Time: 300.000 ms
JIT:
  Functions: 2
  Timing: Generation 1.000 ms, Total 200.000 ms
Execution Time: 210.000 ms
`
	parsed, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	found := ids(Analyze(parsed))
	if _, ok := found["PG113"]; !ok {
		t.Errorf("no PG113: %v", found)
	}
	if _, ok := found["PG114"]; !ok {
		t.Errorf("no PG114: %v", found)
	}
}

func TestInputForms(t *testing.T) {
	autoExplain := `{"Query Text": "select 1", "Plan": {"Node Type": "Result", "Actual Total Time": 1, "Actual Loops": 1, "Actual Rows": 1, "Plan Rows": 1}}`
	if parsed, err := Parse(autoExplain); err != nil || !parsed.Root.Analyzed {
		t.Fatalf("auto_explain: %v", err)
	}
	psql := "                     QUERY PLAN\n---------------------\n Seq Scan on t  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..1.000 rows=1 loops=1)\n Execution Time: 1.000 ms\n(2 rows)\n"
	if parsed, err := Parse(psql); err != nil || parsed.ExecMS != 1 || parsed.Root.Relation != "t" {
		t.Fatalf("psql: %v %+v", err, parsed)
	}
	never := "Hash Join  (cost=1.00..2.00 rows=1 width=4) (actual time=0.1..1.000 rows=0 loops=1)\n  ->  Seq Scan on a  (cost=0.00..1.00 rows=1 width=4) (never executed)\nExecution Time: 1.000 ms\n"
	if parsed, err := Parse(never); err != nil || len(parsed.Root.Children) != 1 {
		t.Fatalf("never executed: %v", err)
	}
	for name, bad := range map[string]string{"empty": "  ", "text": "hello", "json": "[]", "broken": "{oops", "no plan": `{"a":1}`} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestFilterColumns(t *testing.T) {
	got := filterColumns("((created_at > now()) AND (o.status = 3) AND (name = 'x'::text))")
	if strings.Join(got, ",") != "status,name,created_at" {
		t.Fatal(got)
	}
}
