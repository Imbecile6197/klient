//go:build !darwin

package i18n

// platformLang is unused off Darwin; LANG / LC_* cover Linux.
func platformLang() string { return "" }
