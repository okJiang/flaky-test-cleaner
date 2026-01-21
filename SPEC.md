# SPEC — Flaky Test Cleaner for `tikv/pd`

> 本规格文档定义一个 AI 驱动的“发现 → 分析 → 互动推进 → 修复 → Review 跟进 → 合入”的闭环系统，用于在 `tikv/pd` 仓库中自动识别并推动修复 flaky tests。

## 1. 背景与动机

`tikv/pd` 的 CI 偶发失败会造成：
- 合并节奏变慢（re-run / 重新排队）
- 信噪比下降（真实回归被噪音淹没）
- 排障成本升高（日志分散、线索不完整）

本项目通过周期性收集失败日志、判定 flaky、自动创建与维护 issue、参与讨论并在获得明确许可后自动提交修复 PR，来降低上述成本。

## 2. 目标 / 非目标

### 2.1 目标（Goals）
- 自动发现：周期性从 CI 拉取失败 run 的结构化证据。
- 自动归因：识别是否为 flaky，并给出“置信度 + 证据 + 可复现线索”。
- 自动治理：为每个 flaky 建立/维护可追踪的 issue（去重、聚合多次出现、打标签、状态推进）。
- 自动分析：在 issue 中提供根因推断、稳定性建议、最小修复策略与风险提示。
- 自动互动：当有人在 issue/PR 讨论时，提供基于证据的跟进与澄清。
- 自动修复（带闸门）：在满足“人类明确许可”的条件下自动创建修复分支与 PR，并根据 review 意见迭代直至合入或放弃。

### 2.2 非目标（Non-Goals）
- 不替代 maintainer 的最终判断：所有自动化都应可被审核、回滚、禁用。
- 不追求 100% 检测覆盖：优先高置信度、低误报。
- 不在本项目内实现 CI 平台本身的复杂解析器大全：优先支持 GitHub Actions，其他提供可插拔接口。

## 3. 术语与定义

- **CI Run**：一次 CI 执行（例如 GitHub Actions 的 workflow run）。
- **Failure Occurrence**：一次失败的具体发生（某 job/step/test case 失败）。
- **Flaky Test**：同一测试在相同/近似代码状态下不稳定（同 commit 或相近 commit，出现“失败/成功”交替或非确定性错误）。
- **Fingerprint（指纹）**：用于 dedup 的唯一键，描述“同一个 flaky 现象”。
- **Issue Thread**：用于治理该 flaky 的 GitHub Issue。

## 4. 总体架构

### 4.1 高层组件

1. **Scheduler**：周期触发（默认每 3 天；可配置为每周）。
2. **CI Provider**：抽象接口，默认实现 `GitHubActionsProvider`。
3. **LogFetcher**：抓取 run/job/step 日志（带缓存、重试、速率限制）。
4. **FailureExtractor**：从日志中抽取结构化失败片段（test 名称、失败类型、堆栈、平台信息等）。
5. **FlakyClassifier**：规则 + LLM 判定是否 flaky（输出置信度、证据与解释）。
6. **IssueManager**：创建/更新 issue、打标签、去重聚合、维护状态。
7. **AnalysisAgent**：在 issue 中输出可行动的分析与建议。
8. **ConversationAgent**：监听 issue/PR 新互动并作出回应（受策略约束）。
9. **FixAgent**：在满足许可条件后生成修复 PR，并持续更新。
10. **ReviewResponseAgent**：跟进 review comment，产生修复提交或解释。
11. **StateStore**：保存指纹 → issue/PR 状态、历史 run 证据、动作审计。
12. **Observability**：日志、指标、告警、成本统计。

### 4.2 数据流（Discovery → Issue）

Scheduler → CI Provider 列举失败 runs → LogFetcher 拉取日志 → FailureExtractor 结构化 → FlakyClassifier 判定 → IssueManager 去重/创建/更新 → AnalysisAgent 评论。

### 4.3 数据流（Issue/PR → Interaction → Fix）

ConversationAgent 监听互动 →（必要时）补充证据/解释/建议 → 当满足许可条件 → FixAgent 生成 PR → ReviewResponseAgent 迭代 → 合入后归档。

## 5. 外部系统与权限

### 5.1 GitHub 权限最小化

本系统需要的动作：
- 读取：workflow runs、日志（若公开仓库通常可匿名/低权限；私有需要 token）
- 写入：创建 issue、评论、打 label、创建 PR、推送分支、回复 review

建议拆分 token：
- `READ_TOKEN`：仅读 actions/logs
- `WRITE_ISSUE_TOKEN`：issue/comment/label
- `WRITE_CODE_TOKEN`：push 分支 + PR（仅在 Fix 阶段启用）

### 5.2 许可闸门（Human Gate）

**默认策略：系统永不自动写代码，除非满足以下“明确许可信号”之一：**
- Maintainer/committer 在对应 issue 打上 `ai-fix-approved` label
- 或在 issue/PR 评论包含精确短语（可配置）：`/ai-fix`（仅允许特定角色触发）

并且还必须满足：
- FlakyClassifier 置信度 ≥ 阈值（默认 0.75）
- 证据完整：至少 N 次失败 occurrence（默认 N=2）且包含可定位测试标识

若不满足：FixAgent 只允许给“建议 patch（diff 提议）”但不创建 PR（默认禁用，可配置）。

## 6. Flaky 判定策略

### 6.1 输入（Evidence Pack）
每个 Failure Occurrence 的结构化字段（尽量从日志提取）：
- `repo`, `workflow`, `run_id`, `run_url`, `commit_sha`, `branch`
- `job_name`, `runner_os`, `arch`, `rust/go version`（如可得）
- `test_framework`（go test / cargo test / etc）
- `test_name`（尽量提取完整包路径/用例名）
- `error_signature`（简化堆栈/错误消息 hash）
- `raw_excerpt`（受限长度，默认 200-400 行，带关键行优先）

### 6.2 规则层（Heuristics）
规则用于快速过滤：
- 明确“基础设施失败”应归为 `infra-flake`（网络、下载失败、runner 断联、权限/配额）。
- 明确“确定性编译错误/语法错误/缺失符号”不应归为 flaky（更像真实回归）。
- 对于“超时/数据竞争/随机性”类特征加权为 flaky。

### 6.3 LLM 层（LLM-based Classification）
LLM 任务：
- 给出分类：`flaky-test` / `infra-flake` / `likely-regression` / `unknown`
- 输出置信度 `0.0~1.0`
- 输出“证据引用”（指向 run_url + 关键 excerpt 片段 ID）
- 解释：为什么这样判定；还需要哪些信息才能更确定

### 6.4 合并策略
最终分类 = 规则先验 + LLM 判定（可配置权重）。
- 若规则强判定为 regression，则不创建 flaky issue，仅记录。
- 若 LLM=unknown 或低置信度，则创建 `needs-triage` issue（默认关闭；可配置为只记录不发 issue）。

## 7. Fingerprint 与去重

### 7.1 Fingerprint 目标
- 同一 flaky 现象应聚合到同一个 issue。
- 不同 flaky（不同测试或不同错误签名）应分开。

### 7.2 默认 Fingerprint 格式（v1）

`fingerprint_v1 = sha256(repo + test_name + normalized_error_signature + framework + optional_platform_bucket)`

- `normalized_error_signature`：对错误消息做归一化（去掉地址、行号、随机 ID、耗时等噪音），并截断到固定长度。
- `optional_platform_bucket`：仅在明显平台相关（windows/mac/linux）时加入。

### 7.3 去重流程
- 计算 fingerprint → 查询 StateStore
- 若已有 open issue：追加 occurrence、更新统计、必要时 bump 状态
- 若无 issue：创建新 issue

## 8. Issue 生命周期与状态机

### 8.1 Labels（建议默认）
- `flaky-test`：确认是 flaky test
- `infra-flake`：基础设施/环境抖动
- `needs-triage`：低置信度，需要人工确认
- `ai-managed`：该 issue 由本系统维护
- `ai-fix-approved`：允许自动开 PR（许可闸门）
- `fix-in-progress`：已开 PR 或正在尝试修复
- `blocked`：等待外部信息（需要 maintainer 提供）

### 8.2 状态（StateStore）
- `NEW`：首次发现，已创建 issue
- `ANALYZED`：已输出分析与建议
- `WAITING_FOR_HUMAN`：等待讨论/确认/许可
- `APPROVED_TO_FIX`：收到明确许可
- `PR_OPENED`：PR 已创建
- `CHANGES_REQUESTED`：收到 review 修改意见
- `MERGED`：合入
- `CLOSED_WONTFIX`：关闭/无法推进

### 8.3 触发条件（简化）
- NEW → ANALYZED：AnalysisAgent 评论成功
- ANALYZED → WAITING_FOR_HUMAN：默认进入等待
- WAITING_FOR_HUMAN → APPROVED_TO_FIX：label/phrase 触发
- APPROVED_TO_FIX → PR_OPENED：FixAgent 创建 PR 成功
- PR_OPENED → CHANGES_REQUESTED：出现 review request changes
- CHANGES_REQUESTED → PR_OPENED：ReviewResponseAgent 推送修复提交
- PR_OPENED → MERGED：PR merged

## 9. Agent 职责与输出规范

### 9.1 IssueCreatorAgent
输入：classification 结果 + evidence pack + fingerprint
输出：issue title/body（模板化），labels，去重行为（create/update）。

Issue 标题建议：
- `[flaky] <test_name> — <short error signature>`

Issue 正文必须包含：
- 近几次 occurrence 表（run_url/commit/job/test）
- 关键日志片段（短）
- AI 判定（分类/置信度/理由）
- 下一步建议（可行动）

### 9.2 AnalysisAgent
输入：issue 当前内容 + evidence pack + repo 上下文（可选：相关源码片段）
输出：
- 可能根因（分层：最可能/次可能）
- 如何复现（尽可能给命令/环境变量）
- 建议修复路径（最小变更优先）
- 风险/回归面提示

### 9.3 ConversationAgent
输入：新评论/新事件（issue/PR）
策略：
- 只回应“信息补充、澄清、证据链接、执行结果报告”。
- 不与人争辩，不做超出证据的断言。
- 必须引用系统已保存证据或可公开链接。

### 9.4 FixAgent（带闸门）
输入：许可信号 + issue 上下文 + evidence + 目标修复策略
输出：
- 新分支（命名：`ai/flaky/<fingerprint-short>`）
- PR：描述、动机、测试计划、与 issue 关联
- 变更：优先修“测试稳定性”而非改产品逻辑；若必须改产品逻辑需更高门槛（默认禁用）。

### 9.5 ReviewResponseAgent
输入：review comments + PR diff + 相关源码上下文
输出：
- commit（尽量小）或解释性回复
- 若无法满足：明确原因并请求人工介入（不提问，以“需要 maintainer 决策”陈述）

## 10. 存储与数据模型

### 10.1 StateStore
MVP 推荐 SQLite（单文件）或 Postgres（可扩展）。

核心表（概念）：
- `occurrences`：run_id/job/test/error_signature/excerpt_id/timestamp
- `fingerprints`：fingerprint/status/issue_number/pr_number/last_seen/first_seen
- `issues`：issue_number/state/labels/history
- `audit_log`：每次自动动作（时间、动作、对象、结果、错误）
- `costs`：LLM token/调用次数

### 10.2 数据保留
- 日志 excerpt 只保留必要片段，默认保留 90 天。
- 对外可引用的 run_url 永久保存。

## 11. 可观测性与运维

指标（Metrics）：
- `runs_scanned_total`, `failures_extracted_total`
- `flaky_detected_total`（按分类细分）
- `issues_created_total`, `issues_updated_total`
- `pr_created_total`, `pr_merged_total`
- `llm_tokens_total`, `llm_cost_estimate`
- `action_failures_total`（按原因：rate limit/auth/parse error）

日志（Logs）：
- 每个 fingerprint 的完整动作链路（含 run_url、issue/pr id）。

告警（Alerts）：
- 连续 N 次抓取失败
- GitHub rate limit 接近耗尽
- 自动写代码动作失败（需人工关注）

## 12. 失败处理与幂等

- 所有写操作都必须幂等：通过 fingerprint + issue_number 约束避免重复创建。
- 对 GitHub API：遵循 `Retry-After`，指数退避。
- 对 LLM：缓存分类结果（fingerprint + evidence hash），避免重复花费。
- 对日志抓取：本地缓存与 ETag（若支持）。

## 13. 安全与合规

- Secrets 仅通过 CI secrets / 环境变量注入，不落盘。
- 日志 excerpt 在写入 issue 前必须做敏感信息清洗（token、cookie、内部 URL）。
- 提供“紧急停止开关”（配置项）：禁用写操作、禁用 PR。

## 14. 测试策略

- 单元测试：
  - Fingerprint 归一化与稳定性
  - FailureExtractor 的样例日志解析
  - 去重/状态机转移
- 集成测试（可选，使用 GitHub API mock）：
  - issue 创建/更新
  - label 管理

## 15. 里程碑（建议）

### Milestone A — Discover + Issue (MVP)
- 支持 GitHub Actions 拉取失败 runs
- 提取关键失败片段
- 判定与创建/更新 issue（去重）

### Milestone B — Analysis + Interaction
- AnalysisAgent 输出高质量分析
- ConversationAgent 基于事件增量回应

### Milestone C — Fix + PR (gated)
- 实现许可闸门
- 自动开 PR 与最小修复

### Milestone D — Review Loop + Merge
- ReviewResponseAgent 跟进意见
- 合入后自动归档与总结

## 16. 默认决策（可在实现前调整）

- CI Provider：GitHub Actions
- 调度：每 3 天一次（可配每周）
- 存储：SQLite
- LLM 判定阈值：0.75
- 自动修复：默认关闭；仅在 `ai-fix-approved` 或 `/ai-fix` 触发后开启
- 误报控制：低置信度默认不创建 issue（只记录），可配置为创建 `needs-triage`

---

## Appendix A — Issue 模板（草案）

标题：`[flaky] <test_name> — <short signature>`

正文（要点）：
- Summary（AI 判定 + 置信度）
- Evidence（最近 N 次 run 链接 + 关键 excerpt）
- Suspected root causes（分层）
- Suggested next steps（复现/修复）
- Automation notes（fingerprint、系统状态、下一次扫描时间）
