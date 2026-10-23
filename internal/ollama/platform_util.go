package ollama

import "strings"

func hasPathPrefix(name, prefix string) bool {
	return strings.HasPrefix(name, prefix)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), sub)
}
