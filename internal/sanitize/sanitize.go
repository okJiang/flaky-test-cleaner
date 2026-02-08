package sanitize

import "regexp"

var (
	reAuthHeader = regexp.MustCompile(`(?im)^authorization:\s*\S+\s*$`)
	reGitHubPAT  = regexp.MustCompile(`\bgh[ps]_[A-Za-z0-9_]{20,}\b`)
	reTokenParam = regexp.MustCompile(`(?i)(token|access_token|id_token)=([^\s&]+)`)
)

func Scrub(s string) string {
	s = reAuthHeader.ReplaceAllString(s, "authorization: ***")
	s = reGitHubPAT.ReplaceAllString(s, "gh*_***")
	s = reTokenParam.ReplaceAllString(s, "$1=***")
	return s
}
