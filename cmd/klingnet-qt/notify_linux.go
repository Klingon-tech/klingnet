//go:build linux

package main

import "os/exec"

func sendOSNotification(title, body string) {
	_ = exec.Command("notify-send", "-a", "Klingnet", title, body).Start()
}
