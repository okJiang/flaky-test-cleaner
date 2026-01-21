# WORK

## Feature Request

目标：创建一个新项目，用 AI 自动探测并推动修复 `tikv/pd` repo 中的不稳定（flaky）测试，形成从“发现”到“修复合入”的闭环自动化。

期望流程（初步）：
1. 定时（每三天或每周）拉取 CI fail 的日志
2. AI agent 根据日志判断是否为 flaky test，AI 创建 issue
3. 另一个 AI agent 分析 flaky test，在 issue 上 comment（复现线索、可能根因、建议修法）
4. 监测 issue：若有人互动/讨论，则参与讨论，目标是推进解决该 flaky test
5. 当得到 committer 或 reviewer approve 后，派新的 AI agent 自动写代码修测试并提交（PR）
6. 提交代码后，跟 reviewer 讨论并修复 reviewer 的 comment，直到合入

交付物（当前任务）：首先完成一份可评审并可落地的规格文档 `SPEC.md`，随后再进入实现。

## Agent Work Plan

> 说明：此计划会随着探索与实现进展持续更新。

### Task 1 — Spec 文档与决策固化（当前）
- [ ] 建立仓库工作约定文件：`WORK.md`（本文件）
- [ ] 创建 `SPEC.md`：定义范围、架构、组件职责、状态机、权限/安全、可观测性、失败恢复、里程碑
- [ ] 建立知识库目录：`.codex/knowledge/`，记录关键事实与后续实现需要引用的细节（例如 dedup 指纹格式、状态机转移条件等）

### Progress Log
- 2026-01-21：初始化 WORK.md，准备撰写 SPEC.md。
