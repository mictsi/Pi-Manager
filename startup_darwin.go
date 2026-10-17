package main

import "os/exec"

func showStartupError(message string) {
	// Pass the error as an argument rather than interpolating it into AppleScript.
	const script = `on run argv
display alert "Pi-Manager" message (item 1 of argv) as critical
end run`
	_ = exec.Command("/usr/bin/osascript", "-e", script, "--", message).Run()
}
