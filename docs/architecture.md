# Architecture

    EXPLAIN text or JSON --> Parse --> one Node tree
    measure --> time per node, leaving out children
    checkNode, checkWhole --> findings, ranked by severity and time
    Compare --> two reviews --> fixed, new and remaining

- **Time:** Postgres reports per-loop averages. Under Gather, loops include workers running at once, so `measure` divides by the processes sharing the work. Repeats in a nested loop still add up.
- **Severity** comes from a node's share of the query's time. See the thresholds in `review.go`.
- **The skill** has Claude report leads and test fixes, because one plan shows one query.

Key choice: read plan text only, with no database connection. See `decisions/`.
