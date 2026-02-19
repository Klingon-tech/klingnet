//go:build windows

package main

import (
	"os/exec"
	"strings"
)

func sendOSNotification(title, body string) {
	// Escape single quotes for PowerShell string literals.
	title = strings.ReplaceAll(title, "'", "''")
	body = strings.ReplaceAll(body, "'", "''")

	// Use .NET Windows Forms balloon notification — works on all Windows
	// versions (7/8/10/11) without requiring WinRT or BurntToast.
	script := `Add-Type -AssemblyName System.Windows.Forms;` +
		`$n = New-Object System.Windows.Forms.NotifyIcon;` +
		`$n.Icon = [System.Drawing.SystemIcons]::Information;` +
		`$n.BalloonTipTitle = '` + title + `';` +
		`$n.BalloonTipText = '` + body + `';` +
		`$n.Visible = $true;` +
		`$n.ShowBalloonTip(5000);` +
		`Start-Sleep -Milliseconds 5100;` +
		`$n.Dispose()`
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Start()
}
