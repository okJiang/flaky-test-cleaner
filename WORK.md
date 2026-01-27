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

### Task 1 — Spec 文档与决策固化（已完成）
- [x] 建立仓库工作约定文件：`WORK.md`（本文件）
- [x] 创建 `SPEC.md`：定义范围、架构、组件职责、状态机、权限/安全、可观测性、失败恢复、里程碑
- [x] 建立知识库目录：`.codex/knowledge/`，记录关键事实与后续实现需要引用的细节（例如 dedup 指纹格式、状态机转移条件等）

### Task 2 — Go 实现（MVP：Discover → Issue）（已完成）

目标：实现可运行的 Go 程序，打通以下闭环：
- 从 GitHub Actions 拉取 `tikv/pd` 的失败 runs/jobs
- 下载失败 job 日志并抽取结构化 Failure Occurrence（含 excerpt）
- 计算 fingerprint 去重，写入 TiDB Cloud Starter（MySQL 协议 + TLS）
- 幂等创建/更新 GitHub issue（带 labels 与 machine-managed blocks）

子任务拆分：
- [x] 项目骨架：`go.mod` + `cmd/flaky-test-cleaner` + `internal/*` 分层；支持 `--once`/`--dry-run`
- [x] 配置：环境变量与 flag 合并（tokens、repo、workflow、阈值、TiDB DSN/TLS、扫描窗口）
- [x] GitHub API 客户端：
	- 支持 read/write token 分离
	- 具备重试、退避、限速（含 `Retry-After`）
	- 实现 SPEC §6.5 的 4 个 endpoint：workflows / runs / jobs / job logs
- [x] FailureExtractor：
	- 正则识别 go test 失败信号（`[FAIL]`/`--- FAIL:`/`panic:`/`timeout`/`DATA RACE`）
	- 生成 excerpt（固定窗口，默认每段 ≤120 行）
	- 提取 test_name（尽可能）与 error_signature（normalize 后 hash）
- [x] Fingerprint v1：
	- `sha256(repo + test_name + normalized_error_signature + framework + optional_platform_bucket)`
	- normalize 规则先做 MVP（去掉地址/行号/耗时/随机数字等）
- [x] FlakyClassifier：
	- MVP：规则层（infra vs regression vs flaky-ish）
	- 预留 LLM 接口（可选启用，输出置信度/解释/引用）
- [x] StateStore（TiDB Cloud Starter）：
	- 连接池 + 超时；TLS CA 支持
	- 首次启动自动建表迁移（fingerprints/occurrences/issues/audit_log/costs）
	- 幂等写入（fingerprint UNIQUE；occurrence 去重键）
- [x] IssueManager：
	- label 前缀 `flaky-test-cleaner/`
	- issue body 使用 HTML 注释 block 分段，确保可幂等替换
	- 新 occurrence 到来时更新 Evidence 表格与 “last seen”
	- infra-flake 默认不创建/更新 issue（仅记录指标/审计）
- [x] Runner（Scheduler 的 MVP）：
	- `--once` 默认跑一次（建议由外部 cron/CI 调度每 3 天）
	- 未来可扩展 `--interval=72h` 常驻
- [x] 单测与样例：
	- extractor / normalize / fingerprint 的稳定性测试
	- issue body block 更新的幂等测试
	- 提供 sample 日志 fixture（去敏）

完成标准（MVP）：
- `go test ./...` 全绿
- `--dry-run` 可打印将要创建/更新的 issue 与指纹
- 连上真实 GitHub token + TiDB 参数时，可成功创建/更新 issue（可在 README 给出最小运行指南）

### Task 3 — IssueAgent 分析与状态机推进（进行中）

目标：实现 SPEC Milestone B 的“IssueAgent 初次分析”能力，并把指纹状态机从 `ISSUE_OPEN` 推进到 `TRIAGED/WAITING_FOR_SIGNAL`。

子任务拆分：
- [x] 3.1 状态存储扩展
	- 在 `store.FingerprintRecord` 与 TiDB `fingerprints` 表中新增 `state`、`state_changed_at` 字段，默认 `DISCOVERED`。
	- 暴露 `UpdateFingerprintState` API，约束 `DISCOVERED → ISSUE_OPEN → TRIAGED → WAITING_FOR_SIGNAL` 的前缀路径，禁止 `APPROVED_TO_FIX` 之后回退。
	- Memory store 同步实现，补充单元测试覆盖状态迁移。
- [x] 3.2 IssueAgent 初次分析
	- 新建 `internal/issueagent`（或同等命名）模块，输入：fingerprint record + 最近 occurrences + classification。
	- 输出：Markdown 评论，包含根因假设（基于 heuristics/occurrence 关键词）、复现步骤、建议修复路径、风险提示（参考 SPEC §9.2）。
	- Comment 使用 HTML block tag 标记，以便未来幂等更新；提供最小测试验证模板渲染。
- [x] 3.3 Runner 集成
	- 在 issue 创建/更新后，若 fingerprint state= `ISSUE_OPEN`，调用 IssueAgent 发布评论并把状态更新为 `TRIAGED`，随后立即进入 `WAITING_FOR_SIGNAL`。
	- Dry-run 下打印将要发布的评论摘要；真实运行需写入 GitHub 评论并记录 state。
	- 将 IssueAgent 的动作写入 TiDB `audit_log`（action=`issueagent.initial_analysis`）以备观测。
- [ ] 3.4 IssueAgent 深度分析（读代码上下文）
	- [x] 3.4.1 RepoContext: 从 failing `head_sha` 的 mirror 中基于 stack `path:line` / test 定义，提取短 snippet（带 file+sha+line range + Snippet ID: S1/S2/...）。
	- [x] 3.4.2 兼容：无 Copilot SDK 时，deterministic 评论也包含 RepoContext。
	- [x] 3.4.3 Prompt 收紧：要求引用 snippet ID、给复现命令、给 patch plan（可选 diff 草案）、给 maintainer approval checklist。
	- [ ] 3.4.4 RepoContext 扩展：增加函数名/套件方法等检索，snippet 上限提升到 ≤6，并做去重/排序。

### Task 4 — FixAgent 与 RepoWorkspace（待规划）

目标：实现 SPEC Milestone C 的许可闸门、worktree 租赁与 FixAgent 自动开 PR。

子任务（初稿）：
- [x] 4.1 RepoWorkspaceManager
	- 配置：新增 `FTC_WORKSPACE_MIRROR`（默认 `cache/tikv-pd.git`）、`FTC_WORKSPACE_WORKTREES`（默认 `worktrees`）、`FTC_WORKSPACE_MAX`（默认 2），以及自动推导的 remote URL（`https://github.com/<owner>/<repo>.git`）。
	- 能力：`EnsureMirror`（不存在则 `git clone --mirror`，存在则 `git fetch --prune`）；`CatFile` / `ListTree` / `Grep` 等对 mirror 的只读操作。
	- Worktree 租赁：`Acquire(ctx, name, sha)` 创建 `git worktree add --force`，限制并发数；`Release` 触发 `git worktree remove --force` 并清理磁盘；dry-run/错误需返回明确信息。
	- 测试：使用临时 git 仓库验证 clone/fetch、read-only 操作、租赁（含限流）以及 Release 行为。
- [x] 4.2 许可信号监听：解析 `flaky-test-cleaner/ai-fix-approved` label 及 `/ai-fix` 评论，驱动状态转移到 `APPROVED_TO_FIX`。
- [ ] 4.3 FixAgent MVP
	- [x] 4.3.1 FixAgent scaffolding：接入 RepoWorkspaceManager & GitHub client，拿到 `APPROVED_TO_FIX` 指纹，落地基础注释/审计并推进状态到 `PR_OPEN`。
	- [x] 4.3.2 Patch 构建与验证：在 worktree 内执行最小 go test / file edit钩子（Stub 可先记录 todo）。
	- [ ] 4.3.3 PR 自动创建
		- 监听 `PR_OPEN` 指纹，生成 `ai/fix/<fingerprint-short>` 分支，确保 workspace 中的变更会被 commit & push（先 stub，允许 dry-run）。
		- 通过 GitHub API 创建 PR，并在 issue 及 TiDB store 中记录 PR 编号。
		- state: `PR_OPEN` -> `PR_UPDATING` -> `PR_OPEN`，并打上 `flaky-test-cleaner/ai-pr-open` label。

### Task 5 — Review Loop + Merge
- [x] 5.1 PR 状态检测：轮询 `PR_OPEN` 指纹关联的 PR，若已 merge 则自动评论、关闭 issue 并将 state 置为 `MERGED`；若被关闭但未合并则标记 `PR_NEEDS_CHANGES`。
- [x] 5.2 Review 反馈响应：监听 review comments / CI 失败，生成 TODO 与回复，并驱动 state `PR_NEEDS_CHANGES -> PR_UPDATING`。
	- [x] 5.2.1 GitHub API：支持拉取 PR reviews（CHANGES_REQUESTED/APPROVED）与 commit status（CI fail）。
	- [x] 5.2.2 Runner：当 PR 出现“changes requested / CI failure”时，将指纹从 `PR_OPEN` 推进到 `PR_NEEDS_CHANGES`。
	- [x] 5.2.3 FixAgent：对 `PR_NEEDS_CHANGES` 指纹生成更新计划（写入/更新 TODO 文件）、创建/更新 PR 评论，并推进状态 `PR_NEEDS_CHANGES -> PR_UPDATING -> PR_OPEN`。
	- [x] 5.2.4 审计与测试：写入 `audit_log`，为反馈提取与 comment 渲染增加单测。

### Task 6 — Spec 对齐与工程改进（已完成）
- [x] 6.1 PR 被关闭但未合并时，按 SPEC 状态机转移到 `CLOSED_WONTFIX`（而非 `PR_NEEDS_CHANGES`）。

### Task 7 — CI Pipeline 与集成测试（已完成）

目标：为本仓库建立可复用的 CI pipeline（lint/单测/集成测试/覆盖率），并补齐一套“可在 CI 中稳定运行”的集成测试设计与最小实现骨架。

子任务拆分：
- [x] 7.1 CI workflow：push/PR 触发，包含 gofmt/go vet/go test（unit+integration）与缓存策略
- [x] 7.2 集成测试：使用 stub GitHub API server 驱动 Runner 端到端跑通（workflows→runs→jobs→logs→issue/comment），避免真实网络依赖
- [x] 7.3 可测试性改造：为 GitHub client 支持可配置 base URL；Runner 支持依赖注入与延迟初始化（避免测试触发 git clone）
- [x] 7.4 文档：在 `TEST.md` 固化测试分层策略、CI matrix、运行说明与故障排查

### Task 8 — Copilot CLI SDK 集成（进行中）

目标：接入 `github/copilot-sdk`（Go），用于增强 IssueAgent 初次分析评论生成（默认 best-effort 尝试启用；失败自动回退到现有 heuristic 模板）。

子任务拆分：
- [x] 8.1 知识库：补充 Copilot CLI SDK 基本信息与 Go 使用方法到 `.codex/knowledge/`
- [x] 8.2 代码集成：新增 `internal/copilotsdk` wrapper，并通过配置开关接入 `runInitialAnalysis`
- [x] 8.3 文档：README 增加相关环境变量/flag 说明
- [x] 8.4 测试：`go test ./...` 全绿（SDK 集成不引入 CI 依赖）

### Task 9 — Local E2E 验证支持（进行中）

目标：本地跑端到端验证时，可以“读 upstream Actions 日志”但只在 fork 创建/更新 issue/PR；并支持本地 TiDB（无 TLS / 空密码）。

子任务：
- [x] 9.1 Repo read/write 分离：新增 `FTC_GITHUB_WRITE_OWNER/FTC_GITHUB_WRITE_REPO`（及 flags）
- [x] 9.2 TiDB 本地连接：允许空密码；`TIDB_CA_CERT_PATH` 可选（无 CA 时不启用 TLS）
- [x] 9.3 文档：README 补充本地配置示例
- [ ] 9.4 本地 E2E：用真实 `tikv/pd` 失败 run 做一次 dry-run 验证，并确认 `okjiang/pd` 写入路径可用

### Task 10 — 修复 dry-run 误报与可验证性（进行中）

目标：修复 `validate.log` 中大量 `unknown-test` 与非失败日志（如 etcd config/lease timeout）被当作 flaky 的问题，并让 `--dry-run` 输出足够信息以对照 SPEC 核心字段。

约束/补充需求（来自 validate.log 复跑反馈）：
- 只关注 base branch（默认 `main`）的失败：忽略 PR 分支与 `release-*` cherry-pick 的失败。
- 对“明显是回归/未完成代码导致的 CI 失败”（compile/build/undefined 等）不创建/更新 flaky issue。
- 当同一测试存在父测试与子测试多条 FAIL（例如 `TestX` 与 `TestX/subcase`），只保留最细粒度（leaf）的 test 作为 flaky 记录。

子任务：
- [x] 10.1 FailureExtractor：收紧 `timeout` 匹配（仅 test timeout / deadline exceeded 等），避免匹配 `election-timeout/lease-timeout` 等配置文本；并增强 `[FAIL]` 的 test name 提取。
- [x] 10.2 Runner dry-run 输出：打印 classification/置信度、run_url/job/sha/test_name/error_signature 摘要、excerpt 行数/长度，便于 SPEC 校验。
- [x] 10.3 测试：补充样例日志 fixture 覆盖误报场景，`go test ./...` 全绿。
- [x] 10.4 去重：当存在 subtest 时丢弃 parent test occurrence（仅保留 leaf）。
- [x] 10.5 Run filter：只扫描 base branch 的 workflow runs（push event），忽略 `release-*` / PR runs。
- [x] 10.6 Regression filter：`likely-regression` 不创建/更新 issue（仅记录 store）。

### Progress Log
- 2026-01-21：初始化 WORK.md，完成 SPEC.md 与知识库记录。
- 2026-01-21：完成 MVP Go 实现（discover → issue）、测试与文档。
- 2026-01-21：完成 Task 3.1（Fingerprint state 存储扩展 + API）。
- 2026-01-21：完成 Task 3.2（IssueAgent 初次分析模板与测试）。
- 2026-01-21：完成 Task 3.3（Runner 集成 IssueAgent + GitHub 评论 + audit log）。
- 2026-01-21：完成 Task 4.1（RepoWorkspaceManager 基础设施）。
- 2026-01-21：完成 Task 4.2（许可信号监听与状态推进）。
- 2026-01-21：完成 Task 4.3.1（FixAgent scaffolding）。
- 2026-01-21：完成 Task 4.3.2（Patch 构建与最小验证钩子）。
- 2026-01-22：完成 Task 4.3.3（FixAgent 自动创建 PR）与 Task 5.1（PR 状态检测与 issue 自动归档）。
- 2026-01-22：开始 Task 5.2（Review 反馈响应）。
- 2026-01-22：完成 Task 5.2（监听 review/CI 信号并自动 follow-up）。
- 2026-01-22：开始 Task 6（Spec 对齐与工程改进）。
- 2026-01-22：完成 Task 6.1（PR closed 状态与 SPEC 对齐）。
- 2026-01-22：开始 Task 7（CI pipeline 与集成测试）。
- 2026-01-22：完成 Task 7（CI workflow + runner 集成测试 + TEST.md）。
- 2026-01-24：开始 Task 8（Copilot CLI SDK 集成）：写入知识库，准备接入 Go SDK。
- 2026-01-25：Copilot CLI SDK 改为默认 best-effort 启用（失败自动回退），移除 enable 开关。
- 2026-01-24：开始 Task 9：支持本地 E2E（读 upstream，写 fork；本地 TiDB 无 TLS）。
- 2026-01-25：改进 issue 内容（去掉 timestamp 污染签名/标题；Evidence 增加 OS；Occurrence 时间使用 run.CreatedAt），并新增 `make clean/issue` 用于清理验证创建的 issues。
