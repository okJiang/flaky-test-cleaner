# TEST — CI Pipeline 与集成测试设计

本文档定义本仓库的测试分层、CI pipeline 以及集成测试（integration test）的落地方式。

## 1. 目标

- **稳定**：CI 不依赖真实 GitHub/TiDB 网络与权限，避免 flaky。
- **覆盖关键路径**：至少覆盖一次从“拉取 Actions 失败日志 → 抽取失败 → 指纹/入库 → 创建 issue/comment → 状态推进”的端到端链路。
- **可扩展**：后续可在不破坏现有单测的前提下增加“真实外部服务 E2E”（手动触发/有 secrets）。

## 2. 测试分层

### 2.1 Unit Tests（单元测试）

范围：纯函数或可控依赖的模块。

- `internal/extract`：go test 日志解析、excerpt 生成
- `internal/fingerprint`：normalize 与 fingerprint 稳定性
- `internal/issue`：issue body block 幂等与内容规划
- `internal/issueagent`：评论模板渲染与幂等标记
- `internal/workspace`：本地临时 git 仓库验证 worktree 行为（不访问网络）

运行：

- `go test ./...`

### 2.2 Integration Tests（集成测试）

定义：跨多个 internal 模块的“近似真实运行”，但**外部依赖全部 stub/mock**。

本仓库的集成测试策略：

- 使用自定义 `http.RoundTripper` + `http.ServeMux`（基于 `httptest.NewRecorder`）模拟 GitHub REST API
  - 不需要监听本地端口（避免沙箱环境禁用 bind/listen 导致测试失败）
- 使用 `store.NewMemory()` 作为状态存储（避免真实 MySQL/TiDB）
- 通过 runner 的依赖注入入口 `runner.RunOnceWithDeps` 注入 Memory store，测试结束后可直接断言 state
- 通过 `runner.RunOnceDeps` 注入 GitHub client（使用上面的 stub transport）
- **不触发 git clone / worktree**：Runner 内对 workspace/fixagent 做延迟初始化，仅在确实需要 FixAgent 时才触发

当前已落地的集成测试：

- `internal/runner/integration_test.go`：覆盖 workflows → runs → jobs → logs → issue create → issue comment → state 进入 `WAITING_FOR_SIGNAL`

运行：

- `go test ./... -count=1`

### 2.3 E2E Tests（可选，非 CI 默认）

定义：真实访问 GitHub 与（可选）TiDB Cloud Starter。

特点：需要 secrets，且会产生真实 side effects（创建 issue/comment/PR）。

建议作为：

- `workflow_dispatch` 手动触发的 GitHub Actions workflow
- 或本地手动执行（建议默认 `--dry-run`）

不建议纳入 PR CI。

## 3. CI Pipeline 设计

### 3.1 触发条件

- `push`：仅 `main`
- `pull_request`：目标 `main`
- `workflow_dispatch`：手动触发（便于调试）

### 3.2 Pipeline 阶段

1. **Checkout + Setup Go**
   - `actions/setup-go` 读取 `go.mod` 版本
   - 启用 module cache

2. **Formatting gate**
   - `gofmt -l .` 检查

3. **Static checks**
   - `go vet ./...`

4. **Tests**
   - `go test ./... -count=1`
   - 单测 + 集成测试一起跑（集成测试无外部依赖，允许默认跑）

对应配置文件：.github/workflows/ci.yml

## 4. 可测试性设计约定

为保证“集成测试不访问真实网络/不执行 git clone”，做如下约定：

- GitHub client 必须支持可配置 base URL（默认 `https://api.github.com`）
  - env：`FTC_GITHUB_API_BASE_URL`
  - flag：`--github-api-base-url`
- Runner 必须支持依赖注入（至少 Store）
  - 集成测试通过 `runner.RunOnceWithDeps(ctx, cfg, RunOnceDeps{Store: mem, GitHubRead: gh, GitHubIssue: gh})` 注入 Memory store 与 stub GitHub client
- Runner 对 FixAgent/Workspace 相关行为采用延迟初始化
  - Discovery/Issue 阶段不需要 workspace，因此不应触发 `git clone --mirror`

## 5. 故障排查

- CI 失败在 `gofmt`：本地运行 `gofmt -w .`
- CI 失败在 `go test`：建议先本地 `go test ./... -count=1` 复现
- 集成测试出现 `unexpected GitHub API request`：说明 runner 调用了新的 GitHub endpoint，需要在 stub server 中补齐对应路由

## 6. 测试样例目录（覆盖所有功能）

本节给出“可直接落地”的测试样例清单：每个样例都包含**范围/输入/期望**，并映射到具体测试文件（已有或建议新增）。

> 约定：
> - “Unit” = 纯函数或可控依赖
> - “Integration” = 多模块串联 + 外部依赖 stub
> - “E2E” = 真实外部系统（默认不进 CI）

### 6.1 Config（配置加载与校验）

目标：覆盖 env/flag 合并、必填校验、TiDB 开关约束。

- Unit: `internal/config/config_test.go`
   - Case: `FTC_GITHUB_READ_TOKEN` 缺失
      - 输入：`FTC_GITHUB_READ_TOKEN=`
      - 期望：`FromEnvAndFlags` 返回 error
   - Case: dry-run 下无需 `FTC_GITHUB_ISSUE_TOKEN`
      - 输入：`FTC_DRY_RUN=true`、`FTC_GITHUB_READ_TOKEN=...`、`FTC_GITHUB_ISSUE_TOKEN=`
      - 期望：无 error
   - Case: 非 dry-run 必须提供 issue token
      - 输入：`FTC_DRY_RUN=false`、`FTC_GITHUB_READ_TOKEN=...`、`FTC_GITHUB_ISSUE_TOKEN=`
      - 期望：返回 error
   - Case: `FTC_TIDB_ENABLED=true` 时必须提供 TiDB creds + `TIDB_CA_CERT_PATH`
      - 输入：启用 TiDB，但缺失任一必需 env
      - 期望：返回 error

### 6.2 GitHub Client（HTTP 封装、重试与幂等）

目标：覆盖 baseURL 覆写、POST 重试请求体一致性、label 幂等创建处理。

- Unit: `internal/github/client_test.go`
   - Case: `CreateIssue` 在 503/Retry-After 后重试
      - stub：第一次返回 503 + `Retry-After: 0`，第二次返回 201
      - 期望：两次请求 body 相同（title/labels 不丢），最终成功
   - Case: baseURL 末尾 `/` trim
      - 输入：`NewClientWithBaseURL(..., baseURL="http://x/")`
      - 期望：请求 path 仍正确为 `/repos/.../actions/workflows`

建议新增（可选）：

- Unit: `internal/github/client_test.go`
   - Case: `EnsureLabels` 遇到 422 视为 label 已存在
      - stub：`POST /labels` 返回 422
      - 期望：`EnsureLabels` 不返回 error

### 6.3 Sanitizer（敏感信息脱敏）

目标：覆盖 PAT/token/query 参数脱敏规则。

建议新增：

- Unit: `internal/sanitize/sanitize_test.go`
   - Case: Authorization header
      - 输入：包含 `Authorization: Bearer xxx`
      - 期望：替换为 `authorization: ***`
   - Case: `ghp_...` / `ghs_...` token
      - 期望：替换为 `gh*_***`
   - Case: `token=...` / `access_token=...` / `id_token=...`
      - 期望：值替换为 `***`

### 6.4 Extractor（日志抽取）

目标：覆盖 go test 失败信号、test name 提取、excerpt 窗口。

- Unit: `internal/extract/extract_test.go`
   - Case: `--- FAIL:` 识别 + TestName=TestFoo
   - Case: excerpt 非空

建议新增（可选）：

- Unit: `internal/extract/extract_test.go`
   - Case: 多个 FAIL 段落 → 多个 occurrence
   - Case: `panic:` / `timeout` / `DATA RACE` 等 signal

### 6.5 Fingerprint（normalize + 指纹稳定性）

目标：覆盖 normalize 去噪（行号/耗时/hex 等）与 hash 稳定。

- Unit: `internal/fingerprint/fingerprint_test.go`
   - Case: 行号/耗时/hex 被移除

建议新增（可选）：

- Unit: `internal/fingerprint/fingerprint_test.go`
   - Case: 同输入 → 同 fingerprint；TestName 变化 → fingerprint 改变

### 6.6 Classifier（规则分类）

目标：覆盖 flaky/infra/regression/unknown 四类路径。

建议新增：

- Unit: `internal/classify/classify_test.go`
   - Case: `dial tcp ... i/o timeout` → `infra-flake`
   - Case: `undefined:` / `compile` → `likely-regression`
   - Case: `DATA RACE`/`timeout`/`panic:` → `flaky-test`
   - Case: 空文本 → `unknown`

### 6.7 Store（状态机与幂等写入）

目标：覆盖 Memory store 的状态迁移约束与查询。

- Unit: `internal/store/store_test.go`
   - Case: `ListFingerprintsByState` 过滤

建议新增（可选）：

- Unit: `internal/store/store_test.go`
   - Case: 合法迁移（`DISCOVERED → ISSUE_OPEN → TRIAGED → WAITING_FOR_SIGNAL`）
   - Case: 非法迁移（例如 `WAITING_FOR_SIGNAL → PR_OPEN`）返回 error

TiDB store（E2E/可选）：

- E2E: 真实 TiDB Cloud Starter
   - Case: `Migrate` 建表成功、`UpsertFingerprint` 幂等、`LinkIssue` 可读回
   - 建议：仅 `workflow_dispatch` 跑，避免 PR CI 依赖外部服务

### 6.8 Issue Manager（issue body 规划与幂等块）

目标：覆盖标题生成、blocks 存在、evidence 表格与 excerpt 渲染。

- Unit: `internal/issue/issue_test.go`
   - Case: create change + `FTC:SUMMARY_START` block

建议新增（可选）：

- Unit: `internal/issue/issue_test.go`
   - Case: 已有关联 issue number → 走 update
   - Case: classification=unknown/regression → labels=needs-triage

### 6.9 IssueAgent（初次分析评论模板）

目标：覆盖 comment block 标记、复现命令、证据链接。

- Unit: `internal/issueagent/issueagent_test.go`
   - Case: 必备章节齐全
   - Case: `go test ... -count=30 -race` 复现命令存在
   - Case: run link 存在

### 6.10 Workspace（mirror/worktree 生命周期）

目标：覆盖 Ensure/Acquire/Release、并发限制、只读操作。

- Unit: `internal/workspace/manager_test.go`
   - Case: lifecycle（Ensure + CatFile + ListTree + Acquire + Release）
   - Case: worktree 限流（MaxWorktrees=1）

### 6.11 Runner（端到端编排与分支覆盖）

目标：覆盖 discovery→issue→analysis→waiting、infra-flake 跳过、approval signal 状态推进。

- Integration: `internal/runner/integration_test.go`
   - Case: 最小链路跑通（workflows→runs→jobs→logs→issue create→comment）
   - 期望：fingerprint 最终处于 `WAITING_FOR_SIGNAL` 且绑定 issue number

- Integration: `internal/runner/infra_integration_test.go`
   - Case: infra-flake（日志含 `dial tcp ... i/o timeout`）
   - 期望：不创建 issue、不创建 labels，fingerprint class=infra-flake

- Integration: `internal/runner/approval_integration_test.go`
   - Case: label `flaky-test-cleaner/ai-fix-approved` 触发
   - 期望：`WAITING_FOR_SIGNAL → APPROVED_TO_FIX`（dry-run 下不触发 FixAgent）
   - Case: comment `/ai-fix` 触发
   - 期望：同上

建议新增（可选）：

- Integration: runner PR feedback loop
   - Case: PR review=CHANGES_REQUESTED 或 combined status=failure
   - 期望：`PR_OPEN → PR_NEEDS_CHANGES → PR_UPDATING → PR_OPEN`

### 6.12 FixAgent（修复/PR 创建与 follow-up）

目标：覆盖评论渲染、review checklist、follow-up comment block。

- Unit: `internal/fixagent/agent_test.go`
   - Case: preparation comment 包含 run/sha/testSummary
   - Case: feedback checklist 包含 review + CI status
   - Case: follow-up comment block 存在

建议新增（可选，偏 E2E）：

- Integration/E2E: 使用本地临时 git repo + stub GitHub API
   - Case: `Attempt` 在非 dry-run 下会：创建分支、commit、push（需本地 origin）、创建 PR 并写回 store
   - 期望：`PRNumber` 写入 store，issue 增加 `ai-pr-open` label
