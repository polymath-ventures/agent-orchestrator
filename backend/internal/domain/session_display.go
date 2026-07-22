package domain

import "strings"

const maxSessionDisplayNameRunes = 20

// ComposePrimeDisplayName builds the daemon-owned fallback label for prime.
func ComposePrimeDisplayName(projectName string) string {
	name := normalizePrimePrefix(projectName)
	return roleDisplayName(name, " Prime", maxSessionDisplayNameRunes)
}

func normalizePrimePrefix(prefix string) string {
	name := strings.TrimSpace(prefix)
	if len([]rune(name)) <= 3 {
		return strings.ToUpper(name)
	}
	return name
}

func roleDisplayName(projectName, suffix string, limit int) string {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return ""
	}
	if limit <= 0 {
		return projectName + suffix
	}
	suffixRunes := len([]rune(suffix))
	nameLimit := limit - suffixRunes
	if nameLimit <= 0 {
		return capRunes(projectName+suffix, limit)
	}
	cappedName := strings.TrimRight(capRunes(projectName, nameLimit), "- ")
	if cappedName == "" {
		return capRunes(projectName+suffix, limit)
	}
	return cappedName + suffix
}

func capRunes(s string, limit int) string {
	runes := []rune(s)
	if limit <= 0 || len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
