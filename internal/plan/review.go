package plan

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Thresholds keep findings to things worth a person's time. They're judgment calls, stated in the README.
const (
	manyRemovedRows   = 1000  // rows thrown away before a filter is worth an index
	removedRatio      = 2.0   // removed rows for each row kept
	misestimateRatio  = 10.0  // estimates this far off can change the plan's join and scan choices
	misestimateRows   = 1000  // below this, a wrong estimate rarely changes much
	manyLoops         = 1000  // a nested loop's inner side running this often
	manyHeapFetches   = 1000  // an index-only scan visiting the table this often
	manyBlocksRead    = 10000 // about 80 MB read from disk, at the default 8 kB block size
	slowPlanningMS    = 100.0 // planning this slow is noticeable on its own
	slowJITMS         = 100.0 // JIT this slow is noticeable on its own
	highShare         = 0.20  // a node taking this share of the query is a main cause
	lowShare          = 0.05  // below this share, fixing a node can't change much
	smallestImpactMS  = 1.0   // below this, a finding is only low severity
	hottestNodesShown = 5
)

// Severity levels.
const (
	High   = "HIGH"
	Medium = "MEDIUM"
	Low    = "LOW"
)

// Finding is one thing to look at. A plan shows symptoms, so fixes are candidates to test.
type Finding struct {
	ID       string  `json:"id"`
	Severity string  `json:"severity"`
	Node     string  `json:"node"`
	Message  string  `json:"message"`
	Fix      string  `json:"fix"`
	ImpactMS float64 `json:"impact_ms"`
}

// Hot is a node and the time spent in it, leaving out its children.
type Hot struct {
	Node    string  `json:"node"`
	SelfMS  float64 `json:"self_ms"`
	Percent float64 `json:"percent"`
}

// Review is the result for one plan.
type Review struct {
	Analyzed    bool      `json:"analyzed"`
	ExecutionMS float64   `json:"execution_ms"`
	PlanningMS  float64   `json:"planning_ms"`
	Hottest     []Hot     `json:"hottest"`
	Findings    []Finding `json:"findings"`
	Notes       []string  `json:"notes"`
}

type measured struct {
	node    *Node
	totalMS float64
	selfMS  float64
}

// Label names a node the way a person would read it.
func Label(node *Node) string {
	switch {
	case node.Index != "" && node.Relation != "":
		return fmt.Sprintf("%s using %s on %s", node.Type, node.Index, node.Relation)
	case node.Relation != "":
		return node.Type + " on " + node.Relation
	case node.Index != "":
		return node.Type + " on " + node.Index
	}
	return node.Type
}

// measure works out wall-clock time. Under Gather, a node's loops include each worker running
// at once, so its per-loop average is already close to wall-clock time. Dividing by the
// processes sharing the work gives that, while a nested loop's repeats still add up.
func measure(node *Node, sharers float64, out *[]measured) float64 {
	childSharers := sharers
	if node.WorkersLaunched > 0 {
		childSharers = node.WorkersLaunched + 1
	}
	total := node.TotalTime * node.Loops / sharers
	var children float64
	for _, child := range node.Children {
		children += measure(child, childSharers, out)
	}
	self := total - children
	if self < 0 {
		self = 0
	}
	*out = append(*out, measured{node, total, self})
	return total
}

var (
	equalsColumn = regexp.MustCompile(`(?:\w+\.)?"?(\w+)"?\)?(?:::[\w ]+)?\s*=\s`)
	rangeColumn  = regexp.MustCompile(`(?:\w+\.)?"?(\w+)"?\)?(?:::[\w ]+)?\s*(?:<=|>=|<|>)\s`)
)

// filterColumns finds columns compared in a filter, equality first, as an index would want them.
func filterColumns(filter string) []string {
	var columns []string
	seen := map[string]bool{}
	for _, pattern := range []*regexp.Regexp{equalsColumn, rangeColumn} {
		for _, match := range pattern.FindAllStringSubmatch(filter, -1) {
			name := match[1]
			if !seen[name] && !isLiteral(name) {
				seen[name] = true
				columns = append(columns, name)
			}
		}
	}
	return columns
}

func isLiteral(name string) bool {
	return strings.Trim(name, "0123456789") == "" || name == "text" || name == "numeric"
}

func indexHint(node *Node) string {
	columns := filterColumns(node.Filter)
	if node.Relation == "" || len(columns) == 0 {
		return "Consider an index on the columns in this filter"
	}
	return fmt.Sprintf("Candidate to test: CREATE INDEX CONCURRENTLY ON %s (%s)", node.Relation, strings.Join(columns, ", "))
}

func severity(impact, total float64) string {
	if impact < smallestImpactMS || (total > 0 && impact/total < lowShare) {
		return Low
	}
	if total > 0 && impact/total >= highShare {
		return High
	}
	return Medium
}

// Analyze turns a plan into ranked findings.
func Analyze(plan *Plan) Review {
	review := Review{Analyzed: plan.Root.Analyzed, ExecutionMS: plan.ExecMS, PlanningMS: plan.PlanningMS, Hottest: []Hot{}, Findings: []Finding{}, Notes: []string{}}
	if !plan.Root.Analyzed {
		review.Notes = append(review.Notes, "This plan has estimates only, so nothing here is measured. Run EXPLAIN (ANALYZE, BUFFERS), and remember that ANALYZE really runs the query, so wrap writes in a transaction you roll back.")
		return review
	}
	if !plan.HasBuffers {
		review.Notes = append(review.Notes, "Run EXPLAIN (ANALYZE, BUFFERS) to see disk reads as well.")
	}
	var nodes []measured
	rootTotal := measure(plan.Root, 1, &nodes)
	total := plan.ExecMS
	if total == 0 {
		total = rootTotal
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].selfMS > nodes[j].selfMS })
	for index, item := range nodes {
		if index >= hottestNodesShown || item.selfMS <= 0 {
			break
		}
		review.Hottest = append(review.Hottest, Hot{Label(item.node), round(item.selfMS), round(100 * item.selfMS / total)})
	}
	add := func(id, node, message, fix string, impact float64) {
		review.Findings = append(review.Findings, Finding{id, severity(impact, total), node, message, fix, round(impact)})
	}
	for _, item := range nodes {
		checkNode(item, add)
	}
	checkWhole(plan, total, add)
	rank := map[string]int{High: 0, Medium: 1, Low: 2}
	sort.SliceStable(review.Findings, func(i, j int) bool {
		a, b := review.Findings[i], review.Findings[j]
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		return a.ImpactMS > b.ImpactMS
	})
	return review
}

func round(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}

func checkNode(item measured, add func(id, node, message, fix string, impact float64)) {
	node := item.node
	label := Label(node)
	loops := node.Loops
	if loops == 0 {
		return
	}
	removed := node.RemovedByFilter * loops
	kept := node.ActualRows * loops
	if removed >= manyRemovedRows && removed >= removedRatio*kept {
		message := fmt.Sprintf("Read and threw away %.0f rows to keep %.0f, using filter %s.", removed, kept, node.Filter)
		if strings.Contains(node.Type, "Seq Scan") {
			add("PG101", label, message, indexHint(node)+". It helps most when the filter keeps few rows.", item.selfMS)
		} else {
			add("PG102", label, "The index doesn't cover the whole filter. "+message, indexHint(node)+", or add these columns to the index in use.", item.selfMS)
		}
	}
	if byJoin := node.RemovedByJoin * loops; byJoin >= manyRemovedRows && byJoin >= removedRatio*kept {
		add("PG103", label, fmt.Sprintf("The join produced and threw away %.0f rows to keep %.0f.", byJoin, kept), "Look for a join condition that can use an index, or move conditions into the join key.", item.selfMS)
	}
	if check := node.RemovedByCheck * loops; check >= manyRemovedRows {
		add("PG104", label, fmt.Sprintf("The bitmap recheck threw away %.0f rows.", check), "If the plan shows lossy blocks, more work_mem can help. Otherwise, try a more selective index.", item.selfMS)
	}
	if ratio, wrong := misestimate(node); wrong {
		add("PG105", label, fmt.Sprintf("Postgres expected %.0f rows per loop and got %.0f, which is off by %.0f times. Wrong estimates lead to wrong join and scan choices.", node.PlanRows, node.ActualRows, ratio), "Run ANALYZE on the table. If the columns are related, try CREATE STATISTICS. A raised statistics target can help for uneven data.", item.totalMS)
	}
	if node.Type == "Nested Loop" && len(node.Children) == 2 {
		inner := node.Children[1]
		if inner.Loops >= manyLoops {
			add("PG106", label, fmt.Sprintf("The inner side, %s, ran %.0f times.", Label(inner), inner.Loops), "This is fine when each run is very fast and the outer row count is right. If not, check the outer side's estimate, and that the join column has a good index.", item.totalMS)
		}
	}
	if node.SortSpaceType == "Disk" {
		add("PG107", label, fmt.Sprintf("This sort spilled %.0f kB to disk (%s).", node.SortSpaceKB, node.SortMethod), "An index in this sort order can remove the sort. Otherwise, raise work_mem for this query, or return fewer rows.", item.selfMS)
	}
	if node.HashBatches > 1 {
		add("PG108", label, fmt.Sprintf("This hash table split into %.0f batches, so it used disk.", node.HashBatches), "Raise work_mem or hash_mem_multiplier for this query, or join fewer rows.", item.totalMS)
	}
	if node.WorkersPlanned > 0 && node.WorkersLaunched < node.WorkersPlanned {
		add("PG109", label, fmt.Sprintf("It planned %.0f parallel workers and got %.0f.", node.WorkersPlanned, node.WorkersLaunched), "The server may be out of workers. Check max_parallel_workers and how busy the server was.", item.totalMS)
	}
	if fetches := node.HeapFetches * loops; fetches >= manyHeapFetches && strings.Contains(node.Type, "Index Only") {
		add("PG110", label, fmt.Sprintf("This index-only scan still visited the table %.0f times.", fetches), "VACUUM the table to update its visibility map, and check that autovacuum keeps up.", item.selfMS)
	}
	if node.LossyBlocks > 0 {
		add("PG111", label, fmt.Sprintf("The bitmap became lossy for %.0f blocks, so it had to recheck their rows.", node.LossyBlocks), "Raise work_mem for this query, or use a more selective index.", item.selfMS)
	}
	if node.SharedRead >= manyBlocksRead && len(node.Children) == 0 {
		add("PG112", label, fmt.Sprintf("This read %.0f blocks from outside shared buffers, about %.0f MB.", node.SharedRead, node.SharedRead*8/1024), "A cold cache makes this slower than usual, so run it twice before comparing. Reading fewer rows is the lasting fix.", item.selfMS)
	}
}

func misestimate(node *Node) (float64, bool) {
	high, low := node.ActualRows, node.PlanRows
	if low > high {
		high, low = low, high
	}
	if high < misestimateRows/max(node.Loops, 1) || high < 10 {
		return 0, false
	}
	if low < 1 {
		low = 1
	}
	ratio := high / low
	return ratio, ratio >= misestimateRatio
}

func checkWhole(plan *Plan, total float64, add func(id, node, message, fix string, impact float64)) {
	if plan.PlanningMS >= slowPlanningMS && plan.PlanningMS > total {
		add("PG113", "Planning", fmt.Sprintf("Planning took %.1f ms, longer than running the query at %.1f ms.", plan.PlanningMS, total), "Look for many joined tables or partitions, or use a prepared statement.", plan.PlanningMS)
	}
	if plan.JITMS >= slowJITMS {
		add("PG114", "JIT", fmt.Sprintf("JIT compilation took %.1f ms of a %.1f ms query.", plan.JITMS, total), "If this query is short, raise jit_above_cost or turn JIT off for it.", plan.JITMS)
	}
}

// Comparison shows what changed between two runs of a query.
type Comparison struct {
	BeforeMS      float64   `json:"before_ms"`
	AfterMS       float64   `json:"after_ms"`
	ChangePercent float64   `json:"change_percent"`
	Fixed         []Finding `json:"fixed"`
	New           []Finding `json:"new"`
	Remaining     []Finding `json:"remaining"`
	Notes         []string  `json:"notes"`
}

// Compare checks two plans of the same query, such as before and after adding an index.
func Compare(before, after *Plan) Comparison {
	old, fresh := Analyze(before), Analyze(after)
	result := Comparison{BeforeMS: old.ExecutionMS, AfterMS: fresh.ExecutionMS, Fixed: []Finding{}, New: []Finding{}, Remaining: []Finding{}, Notes: []string{}}
	if !old.Analyzed || !fresh.Analyzed {
		result.Notes = append(result.Notes, "At least one plan has estimates only, so timing can't be compared.")
		return result
	}
	if old.ExecutionMS > 0 {
		result.ChangePercent = round(100 * (fresh.ExecutionMS - old.ExecutionMS) / old.ExecutionMS)
	}
	key := func(finding Finding) string { return finding.ID + "|" + finding.Node }
	had, has := map[string]bool{}, map[string]bool{}
	for _, finding := range old.Findings {
		had[key(finding)] = true
	}
	for _, finding := range fresh.Findings {
		has[key(finding)] = true
		if had[key(finding)] {
			result.Remaining = append(result.Remaining, finding)
		} else {
			result.New = append(result.New, finding)
		}
	}
	for _, finding := range old.Findings {
		if !has[key(finding)] {
			result.Fixed = append(result.Fixed, finding)
		}
	}
	result.Notes = append(result.Notes, "One run isn't a benchmark. A cold cache or a busy server can change timing, so repeat before trusting a small change.")
	return result
}
