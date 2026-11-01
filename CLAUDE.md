# slowplan

MCP server and CLI that finds why a PostgreSQL query is slow from its EXPLAIN output.

## Architecture
`internal/plan/plan.go` parses JSON and text into one tree. `internal/plan/review.go` works out time and findings. `internal/server` exposes the MCP tools, and `main.go` the CLI. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet and tests
- `make lint`: build, manifest validation and MCP Inspector checks
- `make demo`: review a real example

## Conventions
- Never connect to a database or use the network.
- Each rule has a stable `PGnnn` ID. Don't reuse IDs.
- Every rule has a real plan in `examples/` in both formats where possible, from made-up tables. Say in the test if one isn't real.
- JSON and text must give the same findings.

## Do and don't
- Do call fixes candidates, since a plan shows one query.
- Don't say a fix will help, or suggest running anything against production.

## Definition of done
`make test` and `make lint` pass, and `CHANGELOG.md`, `ROADMAP.md` and docs are updated when relevant.
