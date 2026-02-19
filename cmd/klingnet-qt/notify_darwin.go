//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

func sendOSNotification(title, body string) {
	// Escape double quotes and backslashes for AppleScript string literals.
	title = strings.ReplaceAll(title, `\`, `\\`)
	title = strings.ReplaceAll(title, `"`, `\"`)
	body = strings.ReplaceAll(body, `\`, `\\`)
	body = strings.ReplaceAll(body, `"`, `\"`)

	script := `display notification "` + body + `" with title "` + title + `"`
	_ = exec.Command("osascript", "-e", script).Start()
}
