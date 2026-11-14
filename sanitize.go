package main

import "strings"

// oneLine makes text from outside TaskLens (app names, command lines, file
// names, crash reports) safe to show in pop-ups, emails and reports: line
// breaks and other control characters become spaces, so a crafted name can't
// add email headers or fake extra lines.
func oneLine(s string) string {
	if strings.IndexFunc(s, isControl) < 0 {
		return s
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if isControl(r) {
			return ' '
		}
		return r
	}, s))
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '\u2028' || r == '\u2029'
}
