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
- Default: SQLite state store
- Entities: occurrences, fingerprints, issues, audit_log, costs (SPEC §10.1)

4) Gating policy (automation safety)
- Default: no automatic code changes unless explicit allow-signal
- Allow-signal: `ai-fix-approved` label or `/ai-fix` phrase by allowed roles (SPEC §5.2)

5) Classification
- Output classes: `flaky-test`, `infra-flake`, `likely-regression`, `unknown` (SPEC §6.3)
- Default LLM confidence threshold: 0.75 (SPEC §16)

6) Dedup fingerprint v1
- Definition: `sha256(repo + test_name + normalized_error_signature + framework + optional_platform_bucket)` (SPEC §7.2)
- Goal: aggregate same flaky phenomenon into a single issue (SPEC §7.1)
