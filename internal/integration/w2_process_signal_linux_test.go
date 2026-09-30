//go:build integration && linux

package integration

import (
	"os/exec"
	"syscall"
)

func w2WasSIGKILL(exit *exec.ExitError) bool {
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}
