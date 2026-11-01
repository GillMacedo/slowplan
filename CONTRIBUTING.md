# Contributing

Thanks for helping. Fork the repo, make your change on a branch, and open a pull request.

## Setup

    make test

You need Go 1.27 or newer. `make lint` also needs Node and Claude Code. The dev container in `.devcontainer/` sets up all three.

## Pull requests
- PRs go through an automatic check before a person reads them: changes over 30 lines need a linked open issue (`Closes #<number>`), code changes need tests and a `CHANGELOG.md` line, `make test` must pass, and a PR can have at most 800 changed lines.
- Keep each one to a single change, with tests.
- Use only made-up data in tests and examples.
- Update `CHANGELOG.md`, `ROADMAP.md` and docs when relevant.

## Working with AI agents
Agents are welcome. `CLAUDE.md` and `AGENTS.md` have the project's instructions, and `.claude/commands/` has common tasks. Say in your PR that an agent helped, and review the result yourself before submitting. You're responsible for what you submit.

By contributing, you agree that your work is licensed under the MIT License.
