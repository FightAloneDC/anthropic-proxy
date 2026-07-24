# Documentation index

Canonical docs for **anthropic-proxy** (as of the current tree). Read these; ignore `docs/archives/`.

| Doc | Audience | What it covers |
|-----|----------|----------------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Agents + maintainers | Packages, request flow, multi-backend, store, reliability |
| [API.md](./API.md) | Agents + integrators | Public HTTP routes, auth, models, translation surface |
| [RESPONSES_API.md](./RESPONSES_API.md) | Agents (Codex / OpenAI Responses) | Responses ↔ Chat Completions, streaming events, custom tools |
| [CONFIGURATION.md](./CONFIGURATION.md) | Operators | `config.yaml` reference |
| [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) | Operators + agents | Common failures, Codex behavior, debug tips |

## Agent rules for this folder

1. **Only read docs listed above** (and this index). Prefer code under `internal/` when docs and code disagree.
2. **Never read `docs/archives/`** — outdated plans and bug reports. User-only history.
3. Do not revive archive plans as if they were the roadmap; implement from current code + user request.
4. After a behavior change that affects public API or config, update the matching doc here in the same change when practical.

## Project entry points

- Root: [README.md](../README.md) — install, quick start, client setup  
- Agent behavior: [AGENTS.md](../AGENTS.md)  
- Changelog: [CHANGELOG.md](../CHANGELOG.md) (may lag; code wins)
