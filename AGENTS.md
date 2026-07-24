# AGENTS.md

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## Global Rules

- Do not auto-commit, push, or tag without explicit permission
- Do not delete files without asking first
- Ask before judging existing configuration as broken
- Do not expand scope beyond the specified focus
- Report outcomes, do not claim success before testing
- Prefer reading existing code before making changes
- If file exceeds 200-300 lines, split or make modular

## Go Rules

- Prefer stdlib over third-party packages
- Minimal main.go — split into packages when > 30 lines
- Exported functions must have doc comments
- Error handling: always check errors, never use _ to ignore
- snake_case for private, CamelCase for exported

## Project-Specific Rules (from CLAUDE.md)

### CRITICAL - NEVER VIOLATE

1. **NO git reset / revert / push --force** — create NEW commits to fix issues instead. Backup commits exist for recovery.
2. **NO /tmp folder** — always use the project working directory for test files, configs, scripts.
3. **NO commit without user approval** — always ask first.
4. **ASK before any destructive operation** — git checkout, file deletion, process killing. When in doubt, ASK FIRST.
5. **Don't kill port 8006** — production proxy. Use other ports for testing.
6. **Don't delete test files** — user invests tokens creating them. Never delete without explicit instruction.
7. **Translate reasoning, don't strip** — move thinking tags to `reasoning_content` field. Never remove/strip thinking tags from output.
8. **NO sudo unless absolutely necessary** — only when user directly requests it. Default to regular user commands.
9. **NEVER COMMAND OR INSTRUCT THE USER** — maintain assistant role. Suggest options instead. Say "Opsi:", "Bisa:", "Opsional:" — not "You should...".

## Documentation (agents)

Canonical docs live under `docs/`. Start at **`docs/README.md`**.

| Allowed | Forbidden |
|---------|-----------|
| `docs/README.md` (index) | **`docs/archives/**` — never open, cite, or “revive” as roadmap** |
| `docs/ARCHITECTURE.md` | Old phase plans / bug reports (all archived) |
| `docs/API.md` | Treating archive content as current truth |
| `docs/RESPONSES_API.md` | |
| `docs/CONFIGURATION.md` | |
| `docs/TROUBLESHOOTING.md` | |

- **Archives are user-only history.** Agents must not read `docs/archives/` for implementation context.
- If docs and code disagree, **code wins**; update the matching live doc when the public API/config changes.
- Codex / Responses custom tools (`exec` → `custom_tool_call`) are documented in `docs/RESPONSES_API.md`.

## Context

- Language: Go (go 1.21)
- Project: anthropic-proxy
- Docs index: `docs/README.md`
- Rules source: MASTER-AGENTS.md + rules.yaml + CLAUDE.md


<!-- gbrain:retrieval-reflex:resolver-rows -->
- retrieval-reflex | a named person/company/project/place becomes the subject; a brain-page pointer appears in context; "who is", "what do we know about", "tell me about"; about to assert a non-trivial detail about a named entity
<!-- /gbrain:retrieval-reflex:resolver-rows -->


---

## enowx-rag memory

This project uses the `enowx-rag` MCP server for per-project memory.

### Before coding

1. Call `rag_retrieve_context` with the project ID `anthropic-proxy` and the user's query.
2. Read the returned context. If relevant, use it to shape your answer or plan.

### After coding

1. Summarize what you changed.
2. Call `rag_index` with useful new facts, design decisions, gotchas, or patterns under project ID `anthropic-proxy`.

Keep chunks concise (one idea per chunk). Use metadata tags like `type:architecture`, `type:decision`, `type:api`, `type:bugfix`, `type:howto`, or `type:snippet`.

