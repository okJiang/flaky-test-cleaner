# Spec Decisions (Source of Truth)

## Files
- Spec: `SPEC.md`
- Work plan: `WORK.md`

## Default Decisions (SPEC §16)

1) CI Provider
- Default: GitHub Actions
- Rationale: minimal integration surface; easiest to start

2) Schedule
- Default: every 3 days
- Alternative: weekly

3) Storage
- Default: TiDB Cloud Starter (Serverless) state store
- Entities: occurrences, fingerprints, issues, audit_log, costs (SPEC §10.1)

4) Gating policy (automation safety)
- Default: no automatic code changes unless explicit allow-signal
- Allow-signal: `ai-fix-approved` label or `/ai-fix` phrase by allowed roles (SPEC §5.2)

5) Classification
- Output classes: `flaky-test`, `infra-flake`, `likely-regression`, `unknown` (SPEC §6.3)
- Default LLM confidence threshold: 0.75 (SPEC §16)
- Infra-flake handling: ignore (no issue management), optionally metrics-only (SPEC §6.4)

7) Labels prefix
- All labels are prefixed with `flaky-test-cleaner/` (SPEC §8.1)

8) Agent merge
- Analysis + conversation: `IssueAgent` (shared context) (SPEC §4.1, §9.2)
- Fix + review response: `FixAgent` (SPEC §4.1, §9.3)

6) Dedup fingerprint v1
- Definition: `sha256(repo + test_name + normalized_error_signature + framework + optional_platform_bucket)` (SPEC §7.2)
- Goal: aggregate same flaky phenomenon into a single issue (SPEC §7.1)

9) State machine constraint
- After `APPROVED_TO_FIX`, new CI occurrences are recorded but MUST NOT trigger a transition to `NEEDS_UPDATE` (SPEC §8.3)
