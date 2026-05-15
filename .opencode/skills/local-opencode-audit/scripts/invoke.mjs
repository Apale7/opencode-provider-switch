#!/usr/bin/env node
import { existsSync, readFileSync } from "node:fs"
import { spawnSync } from "node:child_process"
import path from "node:path"
import process from "node:process"

const BUILTIN_AGENTS = ["build", "plan", "general", "explore"]
const AGENT_ALIASES = new Map([
  ["planner", "plan"],
  ["planning", "plan"],
  ["planagent", "plan"],
  ["builder", "build"],
  ["buildagent", "build"],
  ["coder", "build"],
  ["coding", "build"],
  ["ops", "build"],
])

function parseArgs(argv) {
  const out = { file: [], focus: [], _: [] }
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (!arg.startsWith("--")) {
      out._.push(arg)
      continue
    }

    const eq = arg.indexOf("=")
    const key = eq === -1 ? arg.slice(2) : arg.slice(2, eq)
    const valueFromEquals = eq === -1 ? undefined : arg.slice(eq + 1)
    const isFlag = ["dry-run", "print-command", "allow-nested", "no-default-agent", "thinking", "self-test"].includes(key)
    const value = valueFromEquals ?? (isFlag ? true : argv[++i])

    if (key === "file" || key === "focus") {
      out[key].push(value)
    } else {
      out[key] = value
    }
  }
  return out
}

function stripAnsi(value) {
  return String(value || "").replace(/\x1b\[[0-9;]*m/g, "")
}

function normalize(value) {
  return String(value || "").toLowerCase().replace(/[^a-z0-9]/g, "")
}

function uniq(values) {
  return [...new Set(values.filter(Boolean))]
}

function candidateExecutableNames(base) {
  if (process.platform !== "win32") return [base]
  return [`${base}.exe`, `${base}.ps1`, `${base}.cmd`, base]
}

function resolveExecutable(base) {
  if (process.platform === "win32" && base === "opencode") {
    for (const dir of String(process.env.PATH || "").split(path.delimiter)) {
      if (!dir) continue
      const pkgDir = path.join(dir, "node_modules", "opencode-ai")
      const nativeExe = path.join(pkgDir, "node_modules", "opencode-windows-x64", "bin", "opencode.exe")
      const baselineExe = path.join(pkgDir, "node_modules", "opencode-windows-x64-baseline", "bin", "opencode.exe")
      if (existsSync(nativeExe)) return nativeExe
      if (existsSync(baselineExe)) return baselineExe
    }
  }

  if (base.includes("/") || base.includes("\\")) {
    return existsSync(base) ? base : base
  }

  for (const dir of String(process.env.PATH || "").split(path.delimiter)) {
    if (!dir) continue
    for (const name of candidateExecutableNames(base)) {
      const full = path.join(dir, name)
      if (!existsSync(full)) continue

      const npmBin = path.join(dir, "node_modules", "opencode-ai", "bin", "opencode")
      if (base === "opencode" && existsSync(npmBin)) return npmBin

      return full
    }
  }
  return base
}

function quoteCmdArg(arg) {
  const value = String(arg)
  if (/^[A-Za-z0-9_./:=+-]+$/.test(value)) return value
  return `"${value.replace(/(\\*)"/g, '$1$1\\"').replace(/(\\+)$/g, "$1$1")}"`
}

function commandSpec(commandPath, args) {
  if (process.platform === "win32" && commandPath === "opencode") {
    const commandLine = [commandPath, ...args.map(quoteCmdArg)].join(" ")
    return { command: "cmd.exe", args: ["/d", "/s", "/c", commandLine] }
  }
  if (process.platform === "win32" && /node_modules[\\/]opencode-ai[\\/]bin[\\/]opencode$/i.test(commandPath)) {
    return { command: process.execPath, args: [commandPath, ...args] }
  }
  if (process.platform === "win32" && commandPath.toLowerCase().endsWith(".ps1")) {
    return { command: "pwsh", args: ["-NoProfile", "-File", commandPath, ...args] }
  }
  if (process.platform === "win32" && /\.(cmd|bat)$/i.test(commandPath)) {
    return { command: commandPath, args, options: { shell: true } }
  }
  return { command: commandPath, args }
}

function run(cmd, args, cwd, timeout = 60000) {
  const spec = commandSpec(cmd, args)
  const result = spawnSync(spec.command, spec.args, {
    cwd,
    encoding: "utf8",
    timeout,
    maxBuffer: 20 * 1024 * 1024,
    ...(spec.options || {}),
  })
  return {
    status: result.status,
    error: result.error,
    stdout: stripAnsi(result.stdout),
    stderr: stripAnsi(result.stderr),
  }
}

function listModels(cwd, warnings) {
  const result = run(OPENCODE, ["models"], cwd)
  if (result.error || result.status !== 0) {
    warnings.push(`Could not list models: ${result.error?.message || result.stderr || `exit ${result.status}`}`)
    return []
  }
  return uniq(result.stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.:-]+$/.test(line)))
}

function listAgents(cwd, warnings) {
  const result = run(OPENCODE, ["agent", "list"], cwd)
  if (result.error || result.status !== 0) {
    warnings.push(`Could not list agents: ${result.error?.message || result.stderr || `exit ${result.status}`}`)
    return BUILTIN_AGENTS
  }

  const agents = []
  for (const line of result.stdout.split(/\r?\n/)) {
    const match = line.trim().match(/^([^\s]+) \((primary|subagent)\)$/)
    if (match) agents.push(match[1])
  }
  const uniqueAgents = uniq(agents)
  if (uniqueAgents.length === 0) {
    warnings.push("Could not parse agent list; falling back to built-in agent names.")
    return BUILTIN_AGENTS
  }
  return uniqueAgents
}

function modelPart(candidate) {
  const index = candidate.indexOf("/")
  return index === -1 ? candidate : candidate.slice(index + 1)
}

function isSubsequence(needle, haystack) {
  if (!needle) return false
  let j = 0
  for (let i = 0; i < haystack.length && j < needle.length; i++) {
    if (haystack[i] === needle[j]) j++
  }
  return j === needle.length
}

function score(input, candidate, type) {
  const raw = String(input || "").trim().toLowerCase()
  const n = normalize(input)
  const c = candidate.toLowerCase()
  const cn = normalize(candidate)
  const part = type === "model" ? modelPart(candidate).toLowerCase() : c
  const pn = normalize(part)
  const digits = n.replace(/\D/g, "")
  const candidateDigits = pn.replace(/\D/g, "")
  const tokens = raw.split(/[^a-z0-9]+/).filter(Boolean)

  if (!n) return 0
  if (raw === c) return 100
  if (n === cn) return 96
  if (raw === part) return 94
  if (n === pn) return 92
  if (type === "model" && c.endsWith(`/${raw}`)) return 88
  if (pn.startsWith(n)) return 78
  if (type === "model" && pn.endsWith(n)) return 86
  if (pn.includes(n)) return 72
  if (type === "model" && digits.length >= 2 && candidateDigits === digits) {
    const tokenBonus = tokens.filter((token) => /[a-z]/.test(token) && part.includes(token)).length * 8
    return 70 + tokenBonus
  }
  if (cn.includes(n)) return 66
  if (isSubsequence(n, cn)) return 45
  return 0
}

function resolveCandidate(input, candidates, type, warnings) {
  if (!input) return null

  const query = String(input).trim()
  const effectiveQuery = type === "agent"
    ? AGENT_ALIASES.get(normalize(query)) || query
    : query

  const ranked = candidates
    .map((candidate) => ({ candidate, score: score(effectiveQuery, candidate, type) }))
    .filter((entry) => entry.score > 0)
    .sort((a, b) => b.score - a.score || a.candidate.localeCompare(b.candidate))

  if (ranked.length === 0) {
    if (type === "model" && query.includes("/")) return query
    warnings.push(`No ${type} match for "${query}"; omitting --${type}.`)
    return null
  }

  const best = ranked[0]
  const near = ranked.filter((entry) => entry.candidate !== best.candidate && best.score - entry.score <= 4)
  if (near.length > 0 && best.score < 94) {
    warnings.push(`Ambiguous ${type} "${query}": ${[best, ...near].map((entry) => entry.candidate).join(", ")}; omitting --${type}.`)
    return null
  }

  return best.candidate
}

function resolveAgent(input, cwd, warnings) {
  if (!input) return null

  const query = String(input).trim()
  const alias = AGENT_ALIASES.get(normalize(query))
  if (alias) return alias

  const builtIn = BUILTIN_AGENTS.find((agent) => normalize(agent) === normalize(query))
  if (builtIn) return builtIn

  return resolveCandidate(query, listAgents(cwd, warnings), "agent", warnings)
}

function readMessage(args) {
  if (args["message-file"]) {
    return readFileSync(path.resolve(String(args["message-file"])), "utf8")
  }
  if (args.message) return String(args.message)
  if (args._.length > 0) return args._.join(" ")
  if (!process.stdin.isTTY) return readFileSync(0, "utf8")
  return ""
}

function quoteArg(arg) {
  const value = String(arg)
  if (/^[A-Za-z0-9_./:=+-]+$/.test(value)) return value
  return `"${value.replace(/"/g, '\\"')}"`
}

function buildOpencodeArgs({ args, cwd, message, resolvedModel, resolvedAgent, warnings }) {
  const opencodeArgs = ["run", message || "Dry-run audit message placeholder."]
  if (resolvedModel) opencodeArgs.push("--model", resolvedModel)
  if (resolvedAgent) opencodeArgs.push("--agent", resolvedAgent)
  if (args.format) opencodeArgs.push("--format", String(args.format))
  if (args.dir) opencodeArgs.push("--dir", String(args.dir))
  if (args.attach) opencodeArgs.push("--attach", String(args.attach))
  if (args.username) opencodeArgs.push("--username", String(args.username))
  if (args.password) opencodeArgs.push("--password", String(args.password))
  if (args.port) opencodeArgs.push("--port", String(args.port))
  if (args.variant) opencodeArgs.push("--variant", String(args.variant))
  if (args.thinking) opencodeArgs.push("--thinking")

  for (const file of args.file || []) {
    const filePath = String(file)
    if (!existsSync(path.resolve(cwd, filePath))) {
      warnings.push(`Attached file does not exist locally: ${filePath}`)
    }
    opencodeArgs.push("--file", filePath)
  }

  return opencodeArgs
}

function isLikelyMessageFileError(output, message) {
  const normalizedOutput = stripAnsi(output).replace(/\s+/g, " ")
  const normalizedMessage = String(message || "").trim().replace(/\s+/g, " ")
  if (normalizedMessage.length < 40) return false
  if (!/File not found:/i.test(normalizedOutput)) return false

  const probeLength = Math.min(120, normalizedMessage.length)
  return normalizedOutput.includes(normalizedMessage.slice(0, probeLength))
}

function printMessageFileHint(output, message) {
  if (!isLikelyMessageFileError(output, message)) return
  console.error("[local-opencode-audit] 检测到疑似 `message` 被 `--file` 当成文件路径解析。")
  console.error("[local-opencode-audit] 正确顺序：`opencode run <message> --file <path>`，不要把 `<message>` 放在 `--file` 后面。")
}

function runSelfTest() {
  const warnings = []
  const message = "自测 audit message line 1\nline 2"
  const opencodeArgs = buildOpencodeArgs({
    args: { file: [process.argv[1] || "invoke.mjs"], focus: [] },
    cwd: process.cwd(),
    message,
    resolvedModel: null,
    resolvedAgent: "plan",
    warnings,
  })
  const messageIndex = opencodeArgs.indexOf(message)
  const fileIndex = opencodeArgs.indexOf("--file")
  const errors = []

  if (opencodeArgs[0] !== "run") errors.push("first argument must be run")
  if (messageIndex !== 1) errors.push("message must be immediately after run")
  if (fileIndex !== -1 && fileIndex < messageIndex) errors.push("--file must be after message")

  if (errors.length > 0) {
    console.error(JSON.stringify({ ok: false, errors, args: opencodeArgs, warnings }, null, 2))
    process.exit(1)
  }

  console.log(JSON.stringify({ ok: true, args: opencodeArgs, warnings }, null, 2))
}

const OPENCODE = resolveExecutable(process.env.OPENCODE_BIN || "opencode")

function main() {
  const args = parseArgs(process.argv.slice(2))
  const warnings = []

  if (args["self-test"]) {
    runSelfTest()
    return
  }

  if (process.env.LOCAL_OPENCODE_AUDIT === "1" && !args["allow-nested"]) {
    console.error("Nested local-opencode-audit invocation blocked by LOCAL_OPENCODE_AUDIT=1.")
    process.exit(2)
  }

  const cwd = path.resolve(String(args.cwd || process.cwd()))
  const kind = String(args.kind || "dev").toLowerCase()
  if (!["dev", "ops"].includes(kind)) {
    console.error(`Invalid --kind "${kind}". Use "dev" or "ops".`)
    process.exit(2)
  }

  const message = readMessage(args).trim()
  if (!message && !args["dry-run"]) {
    console.error("No audit message. Pass stdin, --message, or --message-file.")
    process.exit(2)
  }

  const models = args.model ? listModels(cwd, warnings) : []
  const defaultAgent = kind === "ops" ? "build" : "plan"
  const agentInput = args.agent || (args["no-default-agent"] ? null : defaultAgent)
  const resolvedModel = args.model ? resolveCandidate(args.model, models, "model", warnings) : null
  const resolvedAgent = resolveAgent(agentInput, cwd, warnings)

  const opencodeArgs = buildOpencodeArgs({ args, cwd, message, resolvedModel, resolvedAgent, warnings })

  if (warnings.length > 0) {
    for (const warning of warnings) console.error(`[local-opencode-audit] ${warning}`)
  }

  if (args["dry-run"] || args["print-command"]) {
    const payload = {
      cwd,
      command: OPENCODE,
      args: opencodeArgs,
      commandLine: [OPENCODE, ...opencodeArgs].map(quoteArg).join(" "),
      kind,
      resolved: { model: resolvedModel, agent: resolvedAgent },
      warnings,
    }
    console.log(JSON.stringify(payload, null, 2))
    return
  }

  const timeout = Number(args.timeout || 600000)
  const spec = commandSpec(OPENCODE, opencodeArgs)
  const result = spawnSync(spec.command, spec.args, {
    cwd,
    encoding: "utf8",
    stdio: ["inherit", "pipe", "pipe"],
    maxBuffer: 20 * 1024 * 1024,
    timeout,
    ...(spec.options || {}),
    env: {
      ...process.env,
      LOCAL_OPENCODE_AUDIT: "1",
    },
  })

  if (result.stdout) process.stdout.write(result.stdout)
  if (result.stderr) process.stderr.write(result.stderr)

  if (result.error) {
    console.error(`[local-opencode-audit] opencode run failed: ${result.error.message}`)
    process.exit(result.error.code === "ETIMEDOUT" ? 124 : 1)
  }
  if ((result.status ?? 0) !== 0) {
    printMessageFileHint(`${result.stdout || ""}\n${result.stderr || ""}`, message)
  }
  process.exit(result.status ?? 0)
}

main()
