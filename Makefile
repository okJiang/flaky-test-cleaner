.PHONY: clean/issue

# Close validation issues created by flaky-test-cleaner in the fork repo.
# Usage:
#   make clean/issue
#   FTC_CLEAN_REPO=okJiang/pd make clean/issue
clean/issue:
	@set -euo pipefail; \
	repo="$${FTC_CLEAN_REPO:-okJiang/pd}"; \
	ids=$$(gh issue list -R "$$repo" -l "flaky-test-cleaner/ai-managed" --state open -L 200 --json number --jq '.[].number'); \
	if [ -z "$$ids" ]; then echo "no issues to close in $$repo"; exit 0; fi; \
	for id in $$ids; do \
		echo "closing issue #$$id in $$repo"; \
		gh issue close "$$id" -R "$$repo" -c "cleanup: closing flaky-test-cleaner validation issue"; \
	done
