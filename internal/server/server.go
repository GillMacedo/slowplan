// Package server exposes plan review as MCP tools.
package server

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/GillMacedo/slowplan/internal/plan"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keeps a stray huge input from filling the agent's context or memory.
const maxPlanBytes = 20 << 20

// ReviewInput is the input of postgres_plan_review.
type ReviewInput struct {
	Plan string `json:"plan,omitempty" jsonschema:"EXPLAIN output as text or JSON. Give this or path"`
	Path string `json:"path,omitempty" jsonschema:"Path to a file holding EXPLAIN output. Give this or plan"`
}

// CompareInput is the input of postgres_plan_compare.
type CompareInput struct {
	Before     string `json:"before,omitempty" jsonschema:"EXPLAIN output before the change"`
	BeforePath string `json:"before_path,omitempty" jsonschema:"Path to a file with the plan before the change"`
	After      string `json:"after,omitempty" jsonschema:"EXPLAIN output after the change"`
	AfterPath  string `json:"after_path,omitempty" jsonschema:"Path to a file with the plan after the change"`
}

func no() *bool { value := false; return &value }

// New returns the MCP server.
func New(version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "slowplan", Version: version}, nil)
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: no(), IdempotentHint: true, OpenWorldHint: no()}
	mcp.AddTool(server, &mcp.Tool{
		Name:  "postgres_plan_review",
		Title: "Review a PostgreSQL query plan",
		Description: "Finds why a PostgreSQL query is slow from its EXPLAIN output, in text or JSON: where the time goes, and problems such as sequential scans throwing away rows, wrong row estimates, sorts and hashes spilling to disk, and nested loops run many times. " +
			"Use after running EXPLAIN (ANALYZE, BUFFERS). It only reads the text you give it and never connects to a database. Suggested indexes are candidates to test.",
		Annotations: annotations,
	}, review)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "postgres_plan_compare",
		Title:       "Compare two PostgreSQL query plans",
		Description: "Compares EXPLAIN output from before and after a change, such as a new index, and shows the timing change and which problems were fixed, remain or are new. It never connects to a database.",
		Annotations: annotations,
	}, compare)
	return server
}

// Read returns a plan's text from an inline value or a file.
func Read(inline, path string) (*plan.Plan, error) {
	if (inline == "") == (path == "") {
		return nil, errors.New("give the plan's text or a path to a file, not both or neither")
	}
	text := inline
	if path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.Size() > maxPlanBytes {
			return nil, fmt.Errorf("%s is larger than %d MB, so it doesn't look like one plan", path, maxPlanBytes>>20)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		text = string(data)
	}
	return plan.Parse(text)
}

func review(_ context.Context, _ *mcp.CallToolRequest, in ReviewInput) (*mcp.CallToolResult, plan.Review, error) {
	parsed, err := Read(in.Plan, in.Path)
	if err != nil {
		return nil, plan.Review{}, err
	}
	return nil, plan.Analyze(parsed), nil
}

func compare(_ context.Context, _ *mcp.CallToolRequest, in CompareInput) (*mcp.CallToolResult, plan.Comparison, error) {
	before, err := Read(in.Before, in.BeforePath)
	if err != nil {
		return nil, plan.Comparison{}, fmt.Errorf("before: %w", err)
	}
	after, err := Read(in.After, in.AfterPath)
	if err != nil {
		return nil, plan.Comparison{}, fmt.Errorf("after: %w", err)
	}
	return nil, plan.Compare(before, after), nil
}
