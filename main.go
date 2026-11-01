// Command slowplan explains why a PostgreSQL query is slow from its EXPLAIN output.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/GillMacedo/slowplan/internal/plan"
	"github.com/GillMacedo/slowplan/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

const usage = `Usage:
  slowplan review <plan-file>          Find why a query is slow. Use - for stdin
  slowplan compare <before> <after>    Compare two plans of one query
  slowplan serve                       Run as an MCP server over stdio
  slowplan version

Options for review and compare:
  --json             Print JSON
  --fail-on LEVEL    review exits 1 at this severity: high, medium, low or none (default none)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	code := 0
	switch args[0] {
	case "review":
		code, err = reviewCommand(args[1:], stdin, stdout)
	case "compare":
		err = compareCommand(args[1:], stdin, stdout)
	case "serve":
		err = server.New(version).Run(context.Background(), &mcp.StdioTransport{})
	case "version":
		fmt.Fprintln(stdout, version)
	default:
		err = fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
	if err != nil {
		fmt.Fprintf(stderr, "slowplan: %v\n", err)
		return 2
	}
	return code
}

func load(path string, stdin io.Reader) (*plan.Plan, error) {
	if path != "-" {
		return server.Read("", path)
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, err
	}
	return plan.Parse(string(data))
}

func parseFlags(name string, args []string, wanted int) ([]string, bool, string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	asJSON := flags.Bool("json", false, "")
	failOn := flags.String("fail-on", "none", "")
	var paths []string
	for rest := args; ; {
		if err := flags.Parse(rest); err != nil {
			return nil, false, "", err
		}
		if flags.NArg() == 0 {
			break
		}
		paths = append(paths, flags.Arg(0))
		rest = flags.Args()[1:]
	}
	if len(paths) != wanted {
		return nil, false, "", fmt.Errorf("%s takes %d plan files\n%s", name, wanted, usage)
	}
	return paths, *asJSON, *failOn, nil
}

func reviewCommand(args []string, stdin io.Reader, stdout io.Writer) (int, error) {
	paths, asJSON, failOn, err := parseFlags("review", args, 1)
	if err != nil {
		return 0, err
	}
	levels := map[string][]string{"none": {}, "high": {plan.High}, "medium": {plan.High, plan.Medium}, "low": {plan.High, plan.Medium, plan.Low}}
	failing, ok := levels[failOn]
	if !ok {
		return 0, errors.New("--fail-on must be high, medium, low or none")
	}
	parsed, err := load(paths[0], stdin)
	if err != nil {
		return 0, err
	}
	result := plan.Analyze(parsed)
	if asJSON {
		if err := printJSON(stdout, result); err != nil {
			return 0, err
		}
	} else {
		printReview(stdout, result)
	}
	for _, finding := range result.Findings {
		for _, level := range failing {
			if finding.Severity == level {
				return 1, nil
			}
		}
	}
	return 0, nil
}

func compareCommand(args []string, stdin io.Reader, stdout io.Writer) error {
	paths, asJSON, _, err := parseFlags("compare", args, 2)
	if err != nil {
		return err
	}
	before, err := load(paths[0], stdin)
	if err != nil {
		return fmt.Errorf("before: %w", err)
	}
	after, err := load(paths[1], stdin)
	if err != nil {
		return fmt.Errorf("after: %w", err)
	}
	result := plan.Compare(before, after)
	if asJSON {
		return printJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Execution: %.1f ms -> %.1f ms (%+.1f%%)\n", result.BeforeMS, result.AfterMS, result.ChangePercent)
	printList(stdout, "Fixed", result.Fixed)
	printList(stdout, "New", result.New)
	printList(stdout, "Still there", result.Remaining)
	for _, note := range result.Notes {
		fmt.Fprintf(stdout, "\n%s\n", note)
	}
	return nil
}

func printJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printList(stdout io.Writer, title string, findings []plan.Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(stdout, "\n%s:\n", title)
	for _, finding := range findings {
		fmt.Fprintf(stdout, "  %s %s: %s\n", finding.ID, finding.Node, finding.Message)
	}
}

func printReview(stdout io.Writer, result plan.Review) {
	if result.Analyzed {
		fmt.Fprintf(stdout, "Execution %.1f ms, planning %.1f ms\n", result.ExecutionMS, result.PlanningMS)
	}
	if len(result.Hottest) > 0 {
		fmt.Fprintln(stdout, "\nWhere the time goes:")
		for _, hot := range result.Hottest {
			fmt.Fprintf(stdout, "  %5.1f%%  %8.1f ms  %s\n", hot.Percent, hot.SelfMS, hot.Node)
		}
	}
	for _, finding := range result.Findings {
		fmt.Fprintf(stdout, "\n%-6s %s %s\n  %s\n  Try: %s\n", finding.Severity, finding.ID, finding.Node, finding.Message, finding.Fix)
	}
	for _, note := range result.Notes {
		fmt.Fprintf(stdout, "\n%s\n", note)
	}
	if result.Analyzed {
		fmt.Fprintln(stdout, "\nThese are leads from one run. Test a fix on realistic data before relying on it.")
	}
}
