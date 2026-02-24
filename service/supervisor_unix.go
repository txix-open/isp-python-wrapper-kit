//go:build linux || darwin

package service

import (
	"context"
	"github.com/txix-open/isp-kit/log"
	"os/exec"
	"syscall"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		s.logger.Warn(ctx, "failed to get pgid, killing process directly")
		_ = cmd.Process.Kill()
		return
	}
	ctx = log.ToContext(ctx, log.Int("pgid", pgid))

	s.logger.Info(ctx, "sending SIGTERM to process group")
	err = syscall.Kill(-pgid, syscall.SIGTERM)
	if err != nil {
		s.logger.Warn(ctx, "failed to send SIGTERM to process group, kill group", log.Any("error", err))
		s.killGroup(ctx, pgid)
		return
	}

	select {
	case err = <-done:
		s.logProcessExited(ctx, err)
	case <-time.After(shutdownProcessTimeout):
		s.logger.Warn(ctx, "timeout, sending SIGKILL to process group")
		s.killGroup(ctx, pgid)
	}
}

func (s *PySupervisor) killGroup(ctx context.Context, pgid int) {
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err != nil {
		s.logger.Warn(ctx, "failed to send SIGKILL", log.Any("error", err))
	} else {
		s.logger.Info(ctx, "process killed after timeout")
	}
}
