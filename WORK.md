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

### Progress Log
- 2026-01-21：初始化 WORK.md，完成 SPEC.md 与知识库记录。
- 2026-01-21：完成 MVP Go 实现（discover → issue）、测试与文档。
