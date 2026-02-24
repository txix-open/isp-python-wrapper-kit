//go:build windows

package service

import (
	"context"
	"os/exec"
	"syscall"
	"time"

	"github.com/txix-open/isp-kit/log"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{}
}

func (s *PySupervisor) stopProcess(
	ctx context.Context,
	cmd *exec.Cmd,
	done chan error,
) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pid := cmd.Process.Pid
	ctx = log.ToContext(ctx, log.Int("pid", pid))
	s.logger.Info(ctx, "sending SIGTERM to process")

	err := cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		s.logger.Warn(ctx, "failed to send SIGTERM, kill process", log.Any("error", err))
		s.killProcess(ctx, cmd)
		return
	}

	select {
	case err = <-done:
		s.logProcessExited(ctx, err)
	case <-time.After(shutdownProcessTimeout):
		s.logger.Warn(ctx, "timeout, sending SIGKILL to process group")
		s.killProcess(ctx, cmd)
	}
}

func (s *PySupervisor) killProcess(ctx context.Context, cmd *exec.Cmd) {
	err := cmd.Process.Kill()
	if err != nil {
		s.logger.Warn(ctx, "failed to send SIGKILL", log.Any("error", err))
	} else {
		s.logger.Info(ctx, "process killed after timeout")
	}
}
