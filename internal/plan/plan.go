// Package plan reads PostgreSQL EXPLAIN output in JSON or text form.
package plan

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Node is one step of a plan. Times, rows and removed rows are per loop, as Postgres reports them.
type Node struct {
	Type            string  `json:"Node Type"`
	Relation        string  `json:"Relation Name"`
	Alias           string  `json:"Alias"`
	Index           string  `json:"Index Name"`
	Filter          string  `json:"Filter"`
	PlanRows        float64 `json:"Plan Rows"`
	ActualRows      float64 `json:"Actual Rows"`
	Loops           float64 `json:"Actual Loops"`
	TotalTime       float64 `json:"Actual Total Time"`
	RemovedByFilter float64 `json:"Rows Removed by Filter"`
	RemovedByJoin   float64 `json:"Rows Removed by Join Filter"`
	RemovedByCheck  float64 `json:"Rows Removed by Index Recheck"`
	SortMethod      string  `json:"Sort Method"`
	SortSpaceType   string  `json:"Sort Space Type"`
	SortSpaceKB     float64 `json:"Sort Space Used"`
	HashBatches     float64 `json:"Hash Batches"`
	PeakMemoryKB    float64 `json:"Peak Memory Usage"`
	WorkersPlanned  float64 `json:"Workers Planned"`
	WorkersLaunched float64 `json:"Workers Launched"`
	HeapFetches     float64 `json:"Heap Fetches"`
	LossyBlocks     float64 `json:"Lossy Heap Blocks"`
	SharedRead      float64 `json:"Shared Read Blocks"`
	Children        []*Node `json:"Plans"`
	Analyzed        bool    `json:"-"`
}

// Plan is one whole EXPLAIN result.
type Plan struct {
	Root       *Node
	PlanningMS float64
	ExecMS     float64
	JITMS      float64
	HasBuffers bool
	Format     string
}

type jsonPlan struct {
	Plan       *Node    `json:"Plan"`
	PlanningMS *float64 `json:"Planning Time"`
	ExecMS     *float64 `json:"Execution Time"`
	JIT        *struct {
		Timing struct {
			Total float64 `json:"Total"`
		} `json:"Timing"`
	} `json:"JIT"`
}

// Parse accepts EXPLAIN JSON, auto_explain JSON or EXPLAIN text.
func Parse(input string) (*Plan, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "\ufeff"))
	if trimmed == "" {
		return nil, errors.New("the plan is empty")
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		return parseJSON(trimmed)
	}
	return parseText(trimmed)
}

func parseJSON(input string) (*Plan, error) {
	var many []jsonPlan
	var one jsonPlan
	if strings.HasPrefix(input, "[") {
		if err := json.Unmarshal([]byte(input), &many); err != nil {
			return nil, errors.New("this looks like JSON but doesn't parse: " + err.Error())
		}
		if len(many) == 0 {
			return nil, errors.New("the JSON holds no plans")
		}
		one = many[0]
	} else if err := json.Unmarshal([]byte(input), &one); err != nil {
		return nil, errors.New("this looks like JSON but doesn't parse: " + err.Error())
	}
	if one.Plan == nil {
		return nil, errors.New(`the JSON has no "Plan" key, so it isn't EXPLAIN (FORMAT JSON) output`)
	}
	result := &Plan{Root: one.Plan, Format: "json"}
	if one.PlanningMS != nil {
		result.PlanningMS = *one.PlanningMS
	}
	if one.ExecMS != nil {
		result.ExecMS = *one.ExecMS
	}
	if one.JIT != nil {
		result.JITMS = one.JIT.Timing.Total
	}
	markAnalyzed(result.Root)
	result.HasBuffers = strings.Contains(input, `"Shared Hit Blocks"`)
	return result, nil
}

func markAnalyzed(node *Node) {
	node.Analyzed = node.Loops > 0 || node.TotalTime > 0
	for _, child := range node.Children {
		markAnalyzed(child)
	}
}

var (
	nodeLine = regexp.MustCompile(`^(\s*)(->\s+)?(.+?)\s+\(cost=[\d.]+\.\.[\d.]+ rows=(\d+) width=\d+\)(?:\s+\(actual(?: time=[\d.]+\.\.([\d.]+))? rows=([\d.]+) loops=(\d+)\))?(?:\s+\(never executed\))?\s*$`)
	relation = regexp.MustCompile(`^(.*?)(?: using (\S+))?(?: on (\S+)(?: (\S+))?)?$`)
	number   = regexp.MustCompile(`[\d.]+`)
	hashLine = regexp.MustCompile(`Batches:\s*(\d+)`)
	memLine  = regexp.MustCompile(`Memory Usage:\s*(\d+)kB`)
	diskSort = regexp.MustCompile(`Sort Method:\s*([^:]+?)\s+(Disk|Memory):\s*(\d+)kB`)
	lossy    = regexp.MustCompile(`lossy=(\d+)`)
	readLine = regexp.MustCompile(`shared(?: hit=\d+)?(?: read=(\d+))?`)
	jitTotal = regexp.MustCompile(`Total\s+([\d.]+)\s*ms`)
)

func parseText(input string) (*Plan, error) {
	result := &Plan{Format: "text"}
	type open struct {
		indent int
		node   *Node
	}
	var stack []open
	var current *Node
	section := ""
	for _, raw := range strings.Split(input, "\n") {
		line := strings.TrimRight(raw, " \r")
		line = strings.TrimPrefix(line, " ") // psql pads the QUERY PLAN column by one space
		text := strings.TrimSpace(line)
		if text == "" || text == "QUERY PLAN" || strings.HasPrefix(text, "---") || strings.HasPrefix(text, "(") && strings.HasSuffix(text, "rows)") {
			continue
		}
		if match := nodeLine.FindStringSubmatch(line); match != nil {
			section = ""
			node := textNode(match)
			indent := len(match[1])
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 {
				if result.Root != nil {
					return nil, errors.New("this text has more than one plan. Give one at a time")
				}
				result.Root = node
			} else {
				parent := stack[len(stack)-1].node
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, open{indent, node})
			current = node
			continue
		}
		if !strings.HasPrefix(line, " ") {
			section = strings.TrimSuffix(strings.SplitN(text, ":", 2)[0], " Time")
			switch {
			case strings.HasPrefix(text, "Planning Time:"):
				result.PlanningMS = firstNumber(text)
			case strings.HasPrefix(text, "Execution Time:"):
				result.ExecMS = firstNumber(text)
			}
			continue
		}
		if section == "JIT" {
			if match := jitTotal.FindStringSubmatch(text); match != nil {
				result.JITMS, _ = strconv.ParseFloat(match[1], 64)
			}
			continue
		}
		if section == "Planning" || current == nil {
			continue
		}
		applyAttribute(current, text, result)
	}
	if result.Root == nil {
		return nil, errors.New("no plan found. Give the output of EXPLAIN in text or JSON format")
	}
	return result, nil
}

func textNode(match []string) *Node {
	node := &Node{}
	label := relation.FindStringSubmatch(match[3])
	node.Type = strings.TrimPrefix(label[1], "Parallel ")
	node.Index, node.Relation, node.Alias = label[2], label[3], label[4]
	if strings.Contains(node.Type, "Bitmap Index Scan") {
		node.Index, node.Relation = node.Relation, ""
	}
	node.PlanRows, _ = strconv.ParseFloat(match[4], 64)
	if match[6] != "" {
		node.Analyzed = true
		node.TotalTime, _ = strconv.ParseFloat(match[5], 64)
		node.ActualRows, _ = strconv.ParseFloat(match[6], 64)
		node.Loops, _ = strconv.ParseFloat(match[7], 64)
	}
	return node
}

func firstNumber(text string) float64 {
	value, _ := strconv.ParseFloat(number.FindString(text), 64)
	return value
}

func applyAttribute(node *Node, text string, result *Plan) {
	if strings.HasPrefix(text, "Worker ") {
		return // per-worker detail repeats what the node line already totals
	}
	switch {
	case strings.HasPrefix(text, "Filter:"):
		node.Filter = strings.TrimSpace(strings.TrimPrefix(text, "Filter:"))
	case strings.HasPrefix(text, "Rows Removed by Filter:"):
		node.RemovedByFilter = firstNumber(text)
	case strings.HasPrefix(text, "Rows Removed by Join Filter:"):
		node.RemovedByJoin = firstNumber(text)
	case strings.HasPrefix(text, "Rows Removed by Index Recheck:"):
		node.RemovedByCheck = firstNumber(text)
	case strings.HasPrefix(text, "Workers Planned:"):
		node.WorkersPlanned = firstNumber(text)
	case strings.HasPrefix(text, "Workers Launched:"):
		node.WorkersLaunched = firstNumber(text)
	case strings.HasPrefix(text, "Heap Fetches:"):
		node.HeapFetches = firstNumber(text)
	case strings.HasPrefix(text, "Heap Blocks:"):
		if match := lossy.FindStringSubmatch(text); match != nil {
			node.LossyBlocks, _ = strconv.ParseFloat(match[1], 64)
		}
	case strings.HasPrefix(text, "Sort Method:"):
		if match := diskSort.FindStringSubmatch(text); match != nil {
			node.SortMethod, node.SortSpaceType = match[1], match[2]
			node.SortSpaceKB, _ = strconv.ParseFloat(match[3], 64)
		}
	case strings.HasPrefix(text, "Buckets:"):
		if match := hashLine.FindStringSubmatch(text); match != nil {
			node.HashBatches, _ = strconv.ParseFloat(match[1], 64)
		}
		if match := memLine.FindStringSubmatch(text); match != nil {
			node.PeakMemoryKB, _ = strconv.ParseFloat(match[1], 64)
		}
	case strings.HasPrefix(text, "Buffers:"):
		result.HasBuffers = true
		if match := readLine.FindStringSubmatch(text); match != nil && match[1] != "" {
			node.SharedRead, _ = strconv.ParseFloat(match[1], 64)
		}
	}
}
