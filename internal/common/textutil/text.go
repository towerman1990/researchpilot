package textutil

import "unicode/utf8"

// Clip returns at most the requested number of runes.
func Clip(value string, count int) string {
	if utf8.RuneCountInString(value) <= count {
		return value
	}
	return string([]rune(value)[:count])
}
