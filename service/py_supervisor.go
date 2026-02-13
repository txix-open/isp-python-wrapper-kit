package service

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/log"
)

const (
	shutdownProcessTimeout = 5 * time.Second
	restartProcessWaitTime = 2 * time.Second
)

type PySupervisor struct {
	bindingAddress string
	configPath     string
	pyModulePath   string
	logger         log.Logger

	stopCh          chan bool
	configUpdatedCh chan bool
}

func NewPySupervisor(
	bindingAddress string,
	configPath string,
	pyModulePath string,
	logger log.Logger,
) *PySupervisor {
	return &PySupervisor{
		bindingAddress: bindingAddress,
		configPath:     configPath,
		pyModulePath:   pyModulePath,
		logger:         logger,

		stopCh:          make(chan bool, 1),
		configUpdatedCh: make(chan bool, 1),
	}
}

func (p *PySupervisor) Start(ctx context.Context) error {
	select {
	case <-p.configUpdatedCh:
	case <-p.stopCh:
		return nil
	}

	for {
		cmd, err := p.startProcess(ctx)
		if err != nil {
			p.logger.Error(ctx, "failed to start python", log.Any("error", err))
			select {
			case <-time.After(restartProcessWaitTime):
				continue
			case <-p.stopCh:
				return nil
			}
		}

		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()

		select {
		case <-waitCh:
			p.logger.Info(ctx, "python process exited, will restart", log.Any("error", err))
			cmd = nil
			select {
			case <-time.After(restartProcessWaitTime):
				continue
			case <-p.stopCh:
				return nil
			}

		case <-p.configUpdatedCh:
			p.logger.Info(ctx, "new config received, restarting python process")
			p.stopProcess(ctx, cmd)
			cmd = nil

		case <-p.stopCh:
			p.logger.Info(ctx, "stop requested")
			p.stopProcess(ctx, cmd)
			return nil
		}
	}
}

func (p *PySupervisor) UpdateConfig(newConfig []byte) error {
	// nolint:mnd
	err := os.WriteFile(p.configPath, newConfig, 0600)
	if err != nil {
		return errors.WithMessage(err, "write config file")
	}

	select {
	case p.configUpdatedCh <- true:
	default:
	}
	return nil
}

func (p *PySupervisor) Stop() {
	select {
	case p.stopCh <- true:
	default:
	}
}

func (p *PySupervisor) startProcess(ctx context.Context) (*exec.Cmd, error) {
	cmd := exec.Command("uv", "run", p.pyModulePath) // nolint:gosec,noctx

	cmd.Env = append(os.Environ(),
		"BINDING_ADDRESS="+p.bindingAddress,
		"CONFIG_FILE="+p.configPath,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return nil, errors.WithMessage(err, "cmd start")
	}

	p.logger.Info(ctx, "python service started",
		log.String("bindingAddress", p.bindingAddress),
		log.String("configPath", p.configPath),
		log.String("modulePath", p.pyModulePath),
	)

	return cmd, nil
}

func (p *PySupervisor) stopProcess(ctx context.Context, cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	err := cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		p.logger.Warn(ctx, "failed to send SIGTERM", log.Any("error", err))
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(shutdownProcessTimeout):
		err = cmd.Process.Kill()
	case err = <-done:
	}

	if err != nil {
		p.logger.Warn(ctx, "python process did not stop gracefully", log.Any("error", err))
	}
}
