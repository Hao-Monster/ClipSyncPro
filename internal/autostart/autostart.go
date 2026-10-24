package autostart

import "strings"

func commandForExecutable(path string) string {
	return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
}
