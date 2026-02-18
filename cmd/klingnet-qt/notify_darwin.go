//go:build darwin

package main

import "os/exec"

func sendOSNotification(title, body string) {
	script := `display notification "` + body + `" with title "` + title + `"`
	_ = exec.Command("osascript", "-e", script).Start()
}
