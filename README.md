# slowplan: why is this PostgreSQL query slow?

[![npm](https://img.shields.io/npm/v/slowplan)](https://www.npmjs.com/package/slowplan) [![CI](https://github.com/GillMacedo/slowplan/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/slowplan/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**An MCP server and CLI that reads PostgreSQL `EXPLAIN` output and tells you where the time goes and what to try first. It works from the plan text alone, so it never connects to your database.**

A long `EXPLAIN ANALYZE` is hard to read, and a mistake is easy: Postgres reports per-worker averages under parallel plans, and the slowest-looking node often isn't where the time went. `slowplan` works out the real time for each step, and ranks problems such as sequential scans throwing away rows, wrong row estimates, sorts and hashes spilling to disk, and nested loops run many times.

## What you can ask your agent

- "This Postgres query is slow. Here's the `EXPLAIN ANALYZE` output."
- "Why is the planner doing a sequential scan here?"
- "I added an index. Did it help?"
- "What's the first thing to fix in this plan?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/slowplan
    claude plugin install slowplan@slowplan-marketplace

As an MCP server, for any client:

    {"mcpServers": {"slowplan": {"command": "npx", "args": ["-y", "slowplan@0.1.0", "serve"]}}}

As a CLI, with Node 18 or newer:

    npx slowplan review plan.txt

Or with Go 1.27 or newer: `go install github.com/GillMacedo/slowplan@latest`.

## Usage

Get a plan with `EXPLAIN (ANALYZE, BUFFERS) <your query>`, in text or JSON. **`ANALYZE` really runs the query,** so wrap anything that writes in a transaction you roll back.

Here's a real plan from PostgreSQL 17, on made-up tables:

    $ make demo
    Execution 48.9 ms, planning 0.3 ms

    Where the time goes:
       80.8%      39.5 ms  Bitmap Heap Scan on orders
        3.9%       1.9 ms  Bitmap Index Scan on orders_status_idx

    HIGH   PG102 Bitmap Heap Scan on orders
      The index doesn't cover the whole filter. Read and threw away 84857 rows to keep 857, using filter (bucket = 7).
      Try: Candidate to test: CREATE INDEX CONCURRENTLY ON orders (bucket), or add these columns to the index in use.

    These are leads from one run. Test a fix on realistic data before relying on it.

To check a change, compare the plans from before and after:

    slowplan compare before.txt after.txt

On a real example in `examples/`, adding the index that `slowplan` suggested took a query from about 25 ms to 0.1 ms.

Add `--json` for JSON, or `--fail-on high` to exit 1 in CI. Use `-` to read from stdin.

## What it finds

| Rule | Finds |
|---|---|
| PG101 | A sequential scan throwing away most of the rows it reads |
| PG102 | An index that doesn't cover the whole filter |
| PG103 | A join producing rows and then throwing them away |
| PG104 | A bitmap recheck throwing away many rows |
| PG105 | Row estimates off by 10 times or more |
| PG106 | A nested loop's inner side running 1,000 times or more |
| PG107 | A sort spilling to disk |
| PG108 | A hash table split into batches on disk |
| PG109 | Fewer parallel workers than planned |
| PG110 | An index-only scan still visiting the table often |
| PG111 | A bitmap that became lossy |
| PG112 | Reading about 80 MB or more from outside shared buffers |
| PG113 | Planning slower than running the query |
| PG114 | JIT taking 100 ms or more |

Severity comes from the share of the query's time: high is 20% or more, and low is under 5% or under 1 ms. These thresholds are judgment calls.

## MCP tools

| Tool | Does |
|---|---|
| `postgres_plan_review` | Reviews one plan, from text or a file |
| `postgres_plan_compare` | Compares plans from before and after a change |

## Limits

- **Suggested indexes are candidates.** A plan shows one query with one set of values, and not your other indexes or what an index costs on writes. Test on realistic data.
- **One run isn't a benchmark.** A cold cache or a busy server changes timing.
- **It can't see** locks, network time, or anything outside the plan. A plan with estimates only has nothing measured, and `slowplan` says so.
- **Times under parallel plans are estimates,** because Postgres reports averages across workers.
- PostgreSQL only. Text plans from other tools, or heavily edited ones, may not parse. JSON is the most reliable.
- Plans can hold values from your query. `slowplan` stays on your machine, but your MCP client sends tool results to its model.

## FAQ

**How do I read EXPLAIN ANALYZE output?** Give it to `slowplan`, which shows where the time goes.

**Why is Postgres doing a sequential scan?** Often there's no useful index, or the filter keeps so many rows that an index wouldn't help. PG101 shows how many rows were thrown away.

**How do I find out if a sort or hash is using disk?** PG107 and PG108 flag them.

**How is this different from pev2 or postgres-mcp?** [pev2](https://github.com/dalibo/pev2) is a visualizer for people. [postgres-mcp](https://github.com/crystaldba/postgres-mcp) connects to a live database. `slowplan` gives an agent ranked findings from plan text, with no database access.

## Tested on

PG101, PG102, PG104 to PG112 fired on real plans from a PostgreSQL 17 database with made-up tables, which are in `examples/` in both JSON and text. PG103, PG113 and PG114 are only tested on written examples. MCP tool calls were tested through the MCP Inspector. It hasn't been used on a real production query yet.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/slowplan/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes. PRs that change CI, agent settings, dependencies or build files go to the maintainer, as does anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `slowplan` found your slow query, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
