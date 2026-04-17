# OCSWITCH Agent-First CLI Help System

## Summary

`ocswitch` already has a working CLI and a reasonably complete README, but its help
surface is still optimized for a human reading source-adjacent docs, not for an
AI agent trying to configure the tool end-to-end with high reliability.

This PRD defines a narrow product change:

- every user-facing `ocswitch` command must provide `Long` help
- every user-facing `ocswitch` command must provide `Example` help
- help content must be written for **AI agent execution reliability first**
- manual CLI ergonomics remain important, but are secondary to agent clarity

The intended result is that an AI agent can use `ocswitch --help` and nested
`--help` output as a trustworthy local interface contract for discovery,
planning, execution, and recovery during configuration.

## Product Goal

Make `ocswitch` self-describing enough that a capable AI agent can complete normal
configuration workflows without relying on hidden tribal knowledge, source code
inspection, or README-only instructions.

## Core User Need

User wants to say, in effect:

"Configure `ocswitch` for my providers and aliases, then sync OpenCode and tell me
how to run it."

The agent should be able to do that safely by reading CLI help output and
executing commands in order.

## Why This Matters

Current repository state already supports the core workflow:

- add or import upstream providers
- create aliases and bind ordered targets
- validate config via `ocswitch doctor`
- sync aliases into OpenCode via `ocswitch opencode sync`
- run proxy via `ocswitch serve`

But the command tree does not yet expose the full operational contract through
help text. In practice this means:

- some commands only have `Short`
- side effects are not always explained in help output
- default target paths and scope boundaries are not always repeated where used
- the "what to do next" step is often only in README, not in command help
- an agent may need to infer workflow rules from source code instead of help

This is acceptable for an engineering MVP, but not good enough if `ocswitch` wants
to recommend agent-driven configuration as a first-class path.

## Design Principle

Treat CLI help as a **machine-consumable operational interface**, not as a
minimal hint for humans.

That means help text should answer these questions explicitly for each command:

1. What job does this command do?
2. When should an agent call it?
3. What must already exist before calling it?
4. What state does it read or write?
5. What does it intentionally not do?
6. What command usually comes next?
7. What concrete examples should be copied for common workflows?

## Priority Order

When there is a tradeoff, the implementation should optimize for:

1. AI agent understanding and execution reliability
2. Deterministic and explicit side-effect descriptions
3. Copy-paste-safe examples
4. Human readability
5. Brevity

This means slightly longer help is acceptable if it reduces agent mistakes.

## Non-Goals

1. No GUI or local web setup flow in this task.
2. No new interactive wizard in this task.
3. No major command model redesign unless required to support help clarity.
4. No attempt to encode every README detail into root help output.
5. No hidden behavior added only for agents; behavior must stay consistent with
   the real command implementation.

## Scope

### In Scope

All user-facing commands in the current Cobra tree:

- `ocswitch`
- `ocswitch serve`
- `ocswitch doctor`
- `ocswitch provider`
- `ocswitch provider add`
- `ocswitch provider list`
- `ocswitch provider remove`
- `ocswitch provider import-opencode`
- `ocswitch alias`
- `ocswitch alias add`
- `ocswitch alias list`
- `ocswitch alias bind`
- `ocswitch alias unbind`
- `ocswitch alias remove`
- `ocswitch opencode`
- `ocswitch opencode sync`

### Also In Scope

- defining a consistent writing contract for `Long` and `Example`
- deciding which workflow facts must be repeated in help instead of delegated to
  README only
- optional README adjustments if needed to point agents toward `--help` as the
  authoritative interface contract

### Out of Scope

- adding brand-new command groups just for documentation cosmetics
- changing proxy/runtime semantics unrelated to help clarity
- solving remote connectivity testing beyond what already exists

## Agent-First Help Requirements

Every command help page must be written to support agent execution. The text
does not need to be machine-parseable JSON, but it must be operationally
unambiguous.

### Requirement 1: Each command must define `Long`

`Long` must not repeat `Short` mechanically. It must explain behavior,
preconditions, scope, and next-step guidance.

Minimum `Long` contents per command:

1. command purpose
2. state read/write behavior
3. prerequisites or expected prior commands
4. important defaults and path resolution rules
5. behavior boundaries or non-effects
6. recommended next step

### Requirement 2: Each command must define `Example`

`Example` must contain realistic command lines that an agent can copy with minor
substitutions.

Examples must prefer:

- real flag names
- complete command lines
- stable placeholder values
- workflow-oriented ordering
- one example per important usage pattern

Examples must avoid:

- vague ellipses when a concrete placeholder is better
- examples that omit required flags without explanation
- examples that imply unsupported behavior

### Requirement 3: Root and group commands must explain workflow position

Commands like `ocswitch`, `ocswitch provider`, `ocswitch alias`, and `ocswitch opencode` are not
action commands themselves, but they are decision points for an agent.

Their help must answer:

- what subcommands exist
- in what order agents usually use them
- which subcommand to inspect next for each workflow

### Requirement 4: Help must surface side effects explicitly

Whenever a command writes local config or OpenCode config, help must say so.

Examples:

- provider commands write `ocswitch` config
- alias commands write `ocswitch` config
- `opencode sync` writes the target OpenCode config unless `--dry-run`
- `doctor` validates statically and does not call upstream providers
- `serve` starts a long-running local proxy and does not mutate config

### Requirement 5: Help must surface defaults locally

Agents should not need to jump to README or source to know command-local
defaults.

Examples of defaults that must appear where relevant:

- default `ocswitch` config path via `--config`
- default OpenCode sync target resolution order
- default proxy bind address and API key when describing `serve` or workflows

### Requirement 6: Help must be safe for secrets

Help examples may use placeholder keys like `sk-example`, but must never suggest
printing or leaking real secrets. Examples should not instruct agents to copy
config files to chat or expose API keys in logs.

## Content Contract By Command Type

### Root Command: `ocswitch`

`Long` should define the full happy-path workflow:

1. add/import providers
2. create alias and bind targets
3. run `doctor`
4. run `opencode sync`
5. run `serve`

`Example` should include:

- inspect root help
- scratch setup flow
- import-first flow

### Group Command: `provider`

`Long` should explain that providers are upstream OpenAI-compatible endpoints
used by alias targets. It should state that provider definitions live in local
`ocswitch` config and are separate from alias routing.

`Example` should include:

- add provider
- import from OpenCode
- list providers

### Action Command: `provider add`

`Long` should explain:

- creates or updates a provider entry
- `--base-url` must include `/v1`
- omitted mutable fields may preserve existing values on update
- repeated `--header` appends explicit extra headers
- command does not validate upstream reachability

`Example` should include:

- minimal add
- add with API key
- add with repeated headers
- update only base URL of existing provider

### Action Command: `provider list`

`Long` should explain that output is for inspection, redacts keys, and is often
used by agents to confirm imported or saved provider IDs before binding aliases.

`Example` should include:

- plain listing
- listing with explicit `--config`

### Action Command: `provider remove`

`Long` should explain:

- removes provider from `ocswitch` config
- does not automatically clean alias references
- follow-up `doctor` may fail if aliases still reference removed provider

`Example` should include:

- remove provider
- inspect aliases afterward

### Action Command: `provider import-opencode`

`Long` should explain:

- source defaults to global OpenCode config resolution
- only supported import shape is config-defined `@ai-sdk/openai` custom
  providers with `baseURL` and `apiKey`
- unsupported providers are skipped by design
- `--overwrite` changes update semantics

`Example` should include:

- default import
- import from explicit file
- overwrite existing providers

### Group Command: `alias`

`Long` should explain aliases as the primary user-facing abstraction exposed to
OpenCode as `ocswitch/<alias>`, and that target order defines failover priority.

`Example` should include:

- create alias
- bind primary and fallback targets
- list aliases

### Action Command: `alias add`

`Long` should explain:

- creates or updates alias metadata only
- does not add targets
- disabled aliases are not meant for OpenCode exposure until enabled and valid

`Example` should include:

- create enabled alias
- create disabled alias
- update display name

### Action Command: `alias list`

`Long` should explain output semantics:

- alias enabled/disabled state
- target ordering
- target enabled markers
- common use before `doctor` and `opencode sync`

`Example` should include:

- plain listing
- listing under explicit config path

### Action Command: `alias bind`

`Long` should explain:

- appends target in failover order
- provider must already exist
- alias auto-creates if missing
- binding does not test upstream health
- order matters operationally

`Example` should include:

- first target bind
- second fallback bind
- bind disabled target

### Action Command: `alias unbind`

`Long` should explain:

- removes one concrete target tuple from alias
- does not delete the alias itself
- may leave alias invalid if no enabled targets remain

`Example` should include:

- remove one fallback target
- run `doctor` afterward

### Action Command: `alias remove`

`Long` should explain:

- removes entire alias from local config
- future `opencode sync` will stop exposing it in `provider.ocswitch.models`
- does not directly remove model selection already set elsewhere in OpenCode

`Example` should include:

- remove alias
- sync afterward

### Action Command: `doctor`

`Long` should explain:

- performs static validation only
- validates local config structure and OpenCode sync preview assumptions
- does not issue real upstream requests
- should be called before `opencode sync` or `serve`

`Example` should include:

- plain validation
- validation with alternate config path

### Group Command: `opencode`

`Long` should explain that these commands manage the narrow integration boundary
between `ocswitch` and OpenCode, and do not attempt full OpenCode config takeover.

`Example` should include:

- inspect sync help
- run sync dry-run

### Action Command: `opencode sync`

Existing `Long` is a strong starting point but must be aligned to the new help
contract.

`Long` should explain:

- exact write target rules
- what fields are mutated and not mutated
- that aliases become `provider.ocswitch.models`
- meaning of `--dry-run`
- recommended call order around `doctor`

`Example` should include:

- basic sync
- dry-run preview
- sync and set top-level model
- sync and set model plus small model
- sync to explicit target file

### Action Command: `serve`

`Long` should explain:

- starts long-running proxy
- reads validated local config
- requires a valid alias/provider setup first
- handles OpenCode traffic at the configured local base URL
- should generally be called after `doctor` and `opencode sync`

`Example` should include:

- start with defaults
- start with explicit config path

## Writing Rules For `Long`

All `Long` help should follow a shared style so agents can scan it quickly.

Recommended structure:

1. one-sentence job statement
2. short paragraph describing read/write effects
3. short paragraph describing prerequisites and defaults
4. short paragraph describing boundaries or important caveats
5. one-line next step recommendation when useful

Writing rules:

- use direct, operational language
- prefer exact nouns from the product: provider, alias, target, OpenCode config
- say "does" and "does not" explicitly
- repeat important defaults instead of assuming root help was read
- avoid marketing language
- avoid implementation trivia that does not affect execution

## Writing Rules For `Example`

Examples should be optimized for agent reuse.

Rules:

- use fenced multi-line examples where grouping improves clarity
- order examples from safest/common to more advanced
- keep placeholders stable across commands when possible
- use provider IDs and alias names that match repository terminology
- reflect real workflow order

Preferred placeholder vocabulary:

- provider ids: `su8`, `codex`, `relay`
- alias names: `gpt-5.4`, `gpt-5.4-mini`
- api keys: `sk-example`
- local paths: `/path/to/opencode.jsonc`

## Workflow Scenarios The Help Must Support

Help output across the command tree must enable an agent to execute these common
scenarios without README-only dependency.

### Scenario 1: Configure from scratch

1. inspect `ocswitch --help`
2. add one or more providers
3. add alias
4. bind targets in order
5. run `doctor`
6. run `opencode sync`
7. run `serve`

### Scenario 2: Import existing OpenCode provider definitions first

1. inspect `ocswitch provider import-opencode --help`
2. import supported providers
3. list providers
4. create aliases and bindings
5. validate, sync, serve

### Scenario 3: Change only one provider endpoint

1. inspect `provider add --help`
2. update existing provider base URL or key
3. run `doctor`
4. restart `serve` if already running

### Scenario 4: Remove or replace a fallback target safely

1. inspect `alias list`
2. `alias unbind`
3. optionally `provider remove`
4. run `doctor`
5. run `opencode sync` if alias exposure changed

## README Relationship

README remains useful, but after this task its role changes:

- README explains the product and quick-start narrative
- CLI help becomes the authoritative local execution contract
- README may include one short section telling users and agents to prefer
  command-local `--help` for exact behavior and flag usage

README should not be the only place where critical command semantics live.

## Acceptance Criteria

### Functional Acceptance

1. Every user-facing Cobra command in the current `ocswitch` tree defines non-empty
   `Long` and non-empty `Example` help.
2. `ocswitch --help` and all nested `--help` pages expose enough information for an
   agent to discover the intended configuration workflow without reading source.
3. Help text for mutating commands explicitly states what files/config are
   written.
4. Help text for non-mutating commands explicitly states that they do not write
   config or contact upstreams when that distinction matters.
5. `opencode sync --help` explicitly documents default target resolution and the
   non-default behavior of `--set-model` and `--set-small-model`.
6. `provider add --help` explicitly documents `/v1` requirement and update
   semantics for omitted fields.
7. `alias bind --help` explicitly documents auto-create behavior and ordering
   semantics.

### Quality Acceptance

1. Help examples are copy-paste-ready after simple placeholder substitution.
2. Help text does not contain contradictions with actual runtime behavior.
3. Terminology is consistent across all help pages.
4. No command relies only on README for a behavior that materially affects safe
   execution.

### Agent Success Acceptance

This task is successful if a fresh agent can reasonably do the following with
CLI help as its primary reference:

1. determine the canonical happy-path setup order
2. configure providers and aliases
3. understand what `doctor` validates and what it does not
4. sync aliases into OpenCode safely
5. understand when to start `serve`

## Implementation Notes

This PRD does not require a new documentation subsystem. Current Cobra support
is enough:

- `Short`
- `Long`
- `Example`

Implementation should prefer a small, consistent helper style only if needed to
avoid repeated wording mistakes. Do not over-abstract help generation unless the
duplication becomes genuinely harmful.

## Risks

1. Help text may drift from behavior if future command semantics change without
   updating examples.
2. Longer help output may become noisy if not structured carefully.
3. Over-optimizing for agents could make help feel less natural for humans.

## Risk Mitigation

1. Keep help operational and specific, not verbose by default.
2. Prefer one shared writing contract across all commands.
3. Add tests that assert key phrases exist for high-risk commands.
4. Treat help text as behavior-adjacent surface that must change with command
   semantics.

## Future Extensions

These are explicitly out of this task, but compatible with it later:

- `ocswitch setup` guided workflow command
- shell completion tuned for provider/alias names
- structured `--help-format json` for agent-native consumption
- README agent prompt template that mirrors the help contract

## Final Product Decision

`ocswitch` should recommend an **agent-first, CLI-native** configuration workflow.

The CLI itself must become the most reliable place to learn how to configure the
tool. `Long` and `Example` are therefore not documentation polish; they are part
of the product interface.
