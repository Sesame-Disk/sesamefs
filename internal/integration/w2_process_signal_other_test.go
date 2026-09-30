//go:build integration && !linux

package integration

import "os/exec"

// The parent fails required evidence (or skips optional evidence) before
// starting any writer on this platform. Never certify a non-Linux exit.
func w2WasSIGKILL(*exec.ExitError) bool { return false }
