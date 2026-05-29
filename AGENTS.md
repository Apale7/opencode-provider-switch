<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

Use the `/trellis:start` command when starting a new session to:
- Initialize your developer identity
- Understand current project context
- Read relevant guidelines

Use `@/.trellis/` to learn:
- Development workflow (`workflow.md`)
- Project structure guidelines (`spec/`)
- Developer workspace (`workspace/`)

If you're using Codex, project-scoped helpers may also live in:
- `.agents/skills/` for reusable Trellis skills
- `.codex/agents/` for optional custom subagents

Keep this managed block so 'trellis update' can refresh the instructions.

<!-- TRELLIS:END -->
永远用中文与我交流
你可以使用子代理进行并发处理来加速任务执行速度，同时启用的子代理不要超过10个
任何新增的UI都需要考虑i18n支持
<!-- 除非用户明确指出“这是简单任务”“快速完成”，否则你必须在每个功能模块/步骤完成后开启本地 opencode audit进行review，根据review结果反复执行"修改 -> review -> 修改......"的循环，直到review的结论是没有明显问题再执行下一个功能模块的开发或步骤的执行 -->