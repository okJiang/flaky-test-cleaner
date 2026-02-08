package usecase

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
	"github.com/okJiang/flaky-test-cleaner/internal/workspace"
)

type fileLine struct {
	path string
	line int
}

func buildIssueAgentRepoContext(ctx context.Context, ws *workspace.Manager, occ []domain.Occurrence) string {
	sha := pickHeadSHA(occ)
	if strings.TrimSpace(sha) == "" {
		return ""
	}

	ctxWS, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_ = ws.Ensure(ctxWS)

	pairs := extractGoFileLineHints(occ)
	pairs = uniqueFileLines(pairs)
	if len(pairs) > 3 {
		pairs = pairs[:3]
	}

	var sections []string
	for _, p := range pairs {
		path := normalizeRepoPath(p.path)
		if strings.TrimSpace(path) == "" {
			continue
		}
		ok, err := ws.HasPath(ctxWS, sha, path)
		if err != nil || !ok {
			continue
		}
		b, err := ws.CatFile(ctxWS, sha, path)
		if err != nil {
			continue
		}
		start, end, snippet := sliceWithLineNumbers(string(b), p.line, 40)
		id := fmt.Sprintf("S%d", len(sections)+1)
		sections = append(sections, fmt.Sprintf("- %s: %s@%s L%d-L%d\n\n```go\n%s\n```", id, path, shortSHAContext(sha), start, end, snippet))
		if len(sections) >= 3 {
			break
		}
	}

	if len(sections) == 0 {
		testName := pickTestNameFromOccurrences(occ)
		base := strings.Split(strings.TrimSpace(testName), "/")[0]
		if base != "" && base != "unknown-test" {
			matches, err := ws.Grep(ctxWS, sha, fmt.Sprintf("func %s", base))
			if err == nil {
				for _, m := range matches {
					path, line, ok := parseGitGrepLine(m)
					if !ok {
						continue
					}
					b, err := ws.CatFile(ctxWS, sha, path)
					if err != nil {
						continue
					}
					start, end, snippet := sliceWithLineNumbers(string(b), line, 40)
					id := fmt.Sprintf("S%d", len(sections)+1)
					sections = append(sections, fmt.Sprintf("- %s: %s@%s L%d-L%d\n\n```go\n%s\n```", id, path, shortSHAContext(sha), start, end, snippet))
					break
				}
			}
		}
	}

	if len(sections) == 0 {
		return ""
	}

	return "RepoContextSnippets (read-only, from failing commit):\n" + strings.Join(sections, "\n\n")
}

func pickHeadSHA(occ []domain.Occurrence) string {
	for _, o := range occ {
		if strings.TrimSpace(o.HeadSHA) != "" {
			return strings.TrimSpace(o.HeadSHA)
		}
	}
	return ""
}

func pickTestNameFromOccurrences(occ []domain.Occurrence) string {
	for _, o := range occ {
		if strings.TrimSpace(o.TestName) != "" {
			return o.TestName
		}
	}
	return "unknown-test"
}

func extractGoFileLineHints(occ []domain.Occurrence) []fileLine {
	re := regexp.MustCompile(`(?m)([A-Za-z0-9_./\-]+\.go):(\d+)`)
	var out []fileLine
	for i, o := range occ {
		if i >= 3 {
			break
		}
		text := o.ErrorSignature + "\n" + o.Excerpt
		for _, m := range re.FindAllStringSubmatch(text, 6) {
			line, _ := strconv.Atoi(m[2])
			if line <= 0 {
				continue
			}
			out = append(out, fileLine{path: m[1], line: line})
		}
	}
	return out
}

func uniqueFileLines(in []fileLine) []fileLine {
	m := map[string]int{}
	for _, p := range in {
		key := p.path
		if key == "" {
			continue
		}
		if _, ok := m[key]; !ok {
			m[key] = p.line
		}
	}
	var out []fileLine
	for path, line := range m {
		out = append(out, fileLine{path: path, line: line})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].path == out[j].path {
			return out[i].line < out[j].line
		}
		return out[i].path < out[j].path
	})
	return out
}

func normalizeRepoPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.ReplaceAll(p, "\\", "/")

	if idx := strings.LastIndex(p, "github.com/tikv/pd/"); idx >= 0 {
		return p[idx+len("github.com/tikv/pd/"):]
	}
	if idx := strings.LastIndex(p, "/pd/"); idx >= 0 {
		return p[idx+len("/pd/"):]
	}
	if strings.HasPrefix(p, "/") {
		return strings.TrimPrefix(p, "/")
	}
	return p
}

func parseGitGrepLine(line string) (path string, lineNum int, ok bool) {
	first := strings.IndexByte(line, ':')
	if first < 0 {
		return "", 0, false
	}
	second := strings.IndexByte(line[first+1:], ':')
	if second < 0 {
		return "", 0, false
	}
	second = first + 1 + second
	path = line[:first]
	ln, err := strconv.Atoi(line[first+1 : second])
	if err != nil {
		return "", 0, false
	}
	return path, ln, true
}

func sliceWithLineNumbers(src string, centerLine int, radius int) (start, end int, out string) {
	lines := strings.Split(src, "\n")
	if centerLine <= 0 {
		centerLine = 1
	}
	if radius <= 0 {
		radius = 20
	}
	start = centerLine - radius
	if start < 1 {
		start = 1
	}
	end = centerLine + radius
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		b.WriteString(fmt.Sprintf("%4d: %s\n", i, lines[i-1]))
	}
	return start, end, strings.TrimRight(b.String(), "\n")
}

func shortSHAContext(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}
