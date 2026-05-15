---
name: local-opencode-audit
description: Run local `opencode run` audits only after explicit user-enabled milestone flows; use when user says enable local opencode audit, 启用本地审计, 每个模块后审计, or 每步后跑 audit.
---

# Local OpenCode Audit

Use this skill to run a separate local `opencode run` session as an audit pass.

## Activation

Use ONLY when the user explicitly asks for this flow, for example:

- "enable local opencode audit"
- "after each module/step, run an audit"
- "use opencode run to audit milestones"
- "use local-opencode-audit"
- "启用本地审计"
- "开启本地 opencode audit"
- "每个模块后审计"
- "每个功能模块完成后跑 audit"
- "每个步骤后审计"
- "每步后跑 audit"
- "用 local-opencode-audit 审计里程碑"

Do NOT trigger for ordinary development, ordinary checks, or generic code-review requests unless the user specifically wants this local `opencode run` audit flow.

## Trigger Points

Run once after a real milestone is complete:

- Dev milestone: one cohesive feature module, bug-fix module, refactor slice, or final implementation batch is done and edits are paused.
- Ops milestone: one deploy/config/network operation step is done and current config/command output is stable.

Skip when there is no meaningful diff, no changed config, no command output, or the previous audit already covered the exact same snapshot.

## Recursion Guard

Never invoke this skill from inside an audit run.

- If `LOCAL_OPENCODE_AUDIT=1`, do not invoke.
- Every nested audit prompt must say: `Do not call local-opencode-audit, opencode run, or any nested audit.`
- The helper script also blocks nested invocation unless `--allow-nested` is passed. Do not pass it in normal use.

## Task Type

Choose one:

- `dev`: code feature/fix/refactor/test/docs work.
- `ops`: deployment, config, network, service health, availability, or connectivity work.

## Model And Agent

Extract user-requested model/agent from current context if present. User wording may be imprecise.

- If user did not specify a model, omit `--model` and use opencode default config.
- If user did specify a model, let the helper fuzzy-match it against `opencode models`.
- If user did not specify an agent, use default agent by task type:
- Dev default agent: `plan`.
- Ops default agent: `build`.
- If user did specify an agent, let the helper fuzzy-match it against `opencode agent list` plus built-ins.
- If a specified model/agent is ambiguous, omit that flag and report the ambiguity.

## Context To Pass

Keep context minimal and useful. Do not paste whole session history.

Dev audit message must include:

- User requirement and acceptance criteria.
- Completed module/scope.
- Changed files from `git diff --name-only`.
- Key diff summary and exact file/line focus areas.
- Tests/checks already run and results.
- Known constraints, non-goals, and open questions.

Ops audit message must include:

- Operation goal and current step.
- Redacted config values and changed files.
- Endpoints, ports, service names, and expected health behavior.
- Commands run and relevant output.
- Simple availability/connectivity checks requested.
- Rollback or safety constraints.

Redact secrets before passing context: tokens, passwords, cookies, API keys, auth headers, private URLs if sensitive.

If the active session language is Chinese, write the audit message in Chinese except for exact code symbols, file paths, command names, CLI flags, logs, and protocol names.

## Invocation

Preferred helper:

```powershell
@'
<audit message>
'@ | node .opencode/skills/local-opencode-audit/scripts/invoke.mjs --kind dev --model "<user model text>" --agent "<user agent text>" --file path/to/file.go --timeout 600000
```

For ops:

```powershell
@'
<audit message>
'@ | node .opencode/skills/local-opencode-audit/scripts/invoke.mjs --kind ops --timeout 600000
```

If the user did not specify model, omit `--model`. If the user did not specify agent, omit `--agent`; the helper will use `plan` for `dev` and `build` for `ops`.

Repeat `--file` for files that should be attached. Put line numbers in the message, not in `--file`.

Use dry run when checking resolution only:

```powershell
@'
<audit message>
'@ | node .opencode/skills/local-opencode-audit/scripts/invoke.mjs --kind dev --model "gpt 5.5" --dry-run
```

## Prompt Template: Dev

```text
You are running a read-only local audit for a completed development milestone.

Do not edit files. Do not call local-opencode-audit, opencode run, or any nested audit.

Respond in Chinese unless the user explicitly requests another language. Keep severity labels, file paths, line numbers, command names, CLI flags, code symbols, and identifiers unchanged.

Focus on bugs, regressions, missing tests, spec mismatches, and risky edge cases.
Return findings first, ordered by severity.

Severity:
- P1: blocking correctness/security/data-loss issue
- P2: important bug/regression/coverage gap
- P3: minor risk or maintainability concern

Output format:
- <severity> <file>:<line> - <问题> - <修复建议>

If no findings, say: `无审计发现。` Then list residual test gaps in Chinese.

Context:
<requirements, changed files, line focus, diff summary, verification results>
```

## Prompt Template: Ops

```text
You are running a read-only local audit for a completed operations milestone.

Do not edit files. Do not call local-opencode-audit, opencode run, or any nested audit.

Respond in Chinese unless the user explicitly requests another language. Keep severity labels, targets, config keys, command names, CLI flags, endpoint names, service names, and protocol names unchanged.

Check only lightweight availability, connectivity, config consistency, and rollback risk from the provided context.
Do not perform destructive actions.

Return findings first, ordered by severity.

Severity:
- P1: outage/security/destructive risk
- P2: likely availability/connectivity/config issue
- P3: weak signal or follow-up check

Output format:
- <severity> <target/config/command> - <问题> - <修复/检查建议>

If no findings, say: `无审计发现。` Then list residual blind spots in Chinese.

Context:
<operation background, redacted config, endpoints, commands, outputs, expected behavior>
```

## Handling Results

- Keep audit output in current session summary or task log.
- Fix P1/P2 before moving to next milestone unless user explicitly defers.
- P3 may be noted as follow-up.
- If audit fails or times out, report it as a warning with the command context; do not loop forever.
- Do not let audit continue modifying the same files while main work proceeds.
