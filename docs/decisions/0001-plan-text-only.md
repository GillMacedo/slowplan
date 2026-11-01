# 0001: Read plan text only

**Decision:** take EXPLAIN output and never connect to a database.

**Why:** a plan can come from production without giving an agent database access. It also works for someone else's plan, in a ticket or a log. `postgres-mcp` and similar servers already cover live access.

**Alternatives:** a live connection could read table sizes and existing indexes for better advice, but needs credentials and adds risk.
