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

## Agent Work Plan

> 说明：本次从提交 `6831cfcb1d04f34f8c9356bd12c1159b62debe16` 新分支重建代码库，按阶段提交与推送。

### Task 1 — Rewrite 基线与工程骨架（进行中）
- [x] 1.1 从 `6831cfc` 新建独立 worktree + 分支 `codex/rebuild-from-6831cfc`
- [x] 1.2 读取现有规范与知识库，抽取可复用决策（状态机、指纹、闸门策略）
- [x] 1.3 更新 `WORK.md` 为多阶段重建计划并持续维护进度

### Task 2 — Phase A: 基础脚手架（已完成）
- [x] 2.1 初始化 Go module 与目录结构：`cmd` / `internal/config` / `internal/domain` / `internal/ports` / `internal/runtime`
- [x] 2.2 实现主程序入口（signal 优雅退出 + runtime 启动）
- [x] 2.3 实现配置加载与 env/flag 兼容（保留 `FTC_*` 变量）
- [x] 2.4 基础单测与 `go test ./...` 通过

### Task 3 — Phase B/C: Discovery + Store（已完成）
- [x] 3.1 `internal/adapters/github`：Actions 读取、Issue/PR 写接口、重试与限速
- [x] 3.2 `internal/extract` / `internal/fingerprint` / `internal/classify`：失败提取、签名归一、分类
- [x] 3.3 `internal/adapters/store`：Memory + TiDB（迁移、幂等写入、状态机约束）
- [x] 3.4 `internal/usecase/discovery`：拉取 run/job/log 并驱动 issue 规划

### Task 4 — Phase D/E: IssueAgent + 互动审批（已完成）
- [x] 4.1 `internal/issue`：FTC block 幂等渲染
- [x] 4.2 `internal/issueagent`：初次分析评论（deterministic + 可选 Copilot）
- [x] 4.3 `internal/usecase/interaction`：审批信号（label/`/ai-fix`）与 comment watermark

### Task 5 — Phase F/G: Workspace + FixAgent + Review Loop（已完成）
- [x] 5.1 `internal/workspace`：mirror/worktree 生命周期与并发控制
- [x] 5.2 `internal/fixagent`：准备修复、建分支、提交、推送、创建 PR
- [x] 5.3 `internal/usecase/review`：CHANGES_REQUESTED/CI failure 驱动 `PR_NEEDS_CHANGES -> PR_UPDATING -> PR_OPEN`
- [x] 5.4 终态处理：merged 关闭 issue，closed 未合并置 `CLOSED_WONTFIX`

### Task 6 — Phase H/I: 测试、CI、文档与收尾（进行中）
- [x] 6.1 单元/集成测试补齐（discovery + interaction 主链路）
- [x] 6.2 CI workflow、Makefile、README、TEST 对齐
- [x] 6.3 dry-run 验证输出与审计日志检查
- [x] 6.4 每个子任务完成后 commit & push
- [x] 6.5 知识库 `.codex/knowledge/*.md` 记录实现事实与代码位置

## Progress Log
- 2026-02-08：从 `6831cfc` 创建 `worktrees/rebuild-6831cfc` 与分支 `codex/rebuild-from-6831cfc`。
- 2026-02-08：完成规范与现状复盘，开始 Phase A 重建。
- 2026-02-08：完成 Phase A（main/config/domain/ports/runtime/noop + 单测），`go test ./...` 通过。
- 2026-02-08：完成 Phase B/C（GitHub adapter、extract/fingerprint/classify、Memory+TiDB store、DiscoveryOnce）。
- 2026-02-08：完成 Phase D/E/F/G（IssueAgent、审批信号、workspace、FixAgent、PR feedback loop、终态收敛）。
- 2026-02-08：完成 Phase H 主要交付（README/TEST/Makefile/CI），当前 `go test ./... -count=1` 全绿。
- 2026-02-08：提交并推送 `phase-a: scaffold runtime/config/domain/ports` 与 `rewrite: implement full flaky cleaner architecture` 到 `origin/codex/rebuild-from-6831cfc`。
