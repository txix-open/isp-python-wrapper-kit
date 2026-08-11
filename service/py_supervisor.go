package service

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/log"
)

const (
	shutdownProcessTimeout = 5 * time.Second
	restartProcessWaitTime = 2 * time.Second
)

type InnerRepo interface {
	ReceiveModuleAddresses(ctx context.Context, moduleName string, hosts []string) error
}

type HealthWaiterService interface {
	Wait(ctx context.Context) error
}

type upgradeHostsEvent struct {
	module string
	hosts  []string
}

type PySupervisor struct {
	bindingAddress string
	configPath     string
	pyModulePath   string
	logger         log.Logger
	cancel         context.CancelFunc

	innerRepo    InnerRepo
	modulesHosts map[string][]string

	configUpdatedCh chan bool
	upgradeCh       chan upgradeHostsEvent
	wg              sync.WaitGroup

	healthWaiter HealthWaiterService
}

func NewPySupervisor(
	bindingAddress string,
	configPath string,
	pyModulePath string,
	innerRepo InnerRepo,
	requiredModules []string,
	healthWaiter HealthWaiterService,
	logger log.Logger,
) *PySupervisor {
	return &PySupervisor{
		bindingAddress: bindingAddress,
		configPath:     configPath,
		pyModulePath:   pyModulePath,
		innerRepo:      innerRepo,
		healthWaiter:   healthWaiter,
		logger:         logger,

		modulesHosts:    make(map[string][]string, len(requiredModules)),
		configUpdatedCh: make(chan bool, 1),
		upgradeCh:       make(chan upgradeHostsEvent, len(requiredModules)),
	}
}

func (s *PySupervisor) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	ctx = log.ToContext(ctx, log.String("worker", "supervisor"))

	s.wg.Add(1)
	defer s.wg.Done()

	s.processLoop(ctx)
	return nil
}

func (s *PySupervisor) UpdateConfig(newConfig []byte) error {
	// nolint:mnd
	err := os.WriteFile(s.configPath, newConfig, 0600)
	if err != nil {
		return errors.WithMessage(err, "write config file")
	}

	select {
	case s.configUpdatedCh <- true:
	default:
	}
	return nil
}

func (s *PySupervisor) Close() error {
	s.cancel()
	s.wg.Wait()
	return nil
}

func (s *PySupervisor) Upgrade(module string, hosts []string) {
	s.upgradeCh <- upgradeHostsEvent{module, hosts}
}

func (s *PySupervisor) processLoop(ctx context.Context) {
	var cmd *exec.Cmd
	var exitCh chan error

	for {
		select {
		case ev := <-s.upgradeCh:
			s.modulesHosts[ev.module] = ev.hosts

			if cmd == nil {
				continue
			}

			s.logger.Info(ctx, "apply hosts upgrade", log.String("module", ev.module))
			err := s.innerRepo.ReceiveModuleAddresses(ctx, ev.module, ev.hosts)
			if err != nil {
				s.logger.Error(ctx, "failed to apply hosts", log.String("module", ev.module), log.Any("error", err))
			}
		case <-s.configUpdatedCh:
			s.stopProcess(ctx, cmd, exitCh)
			cmd, exitCh = s.ensureProcessRunning(ctx)

		case err := <-exitCh:
			s.logProcessExited(ctx, err)
			if !s.waitRestart(ctx) {
				return
			}

			s.logger.Info(ctx, "restart process")
			cmd, exitCh = s.ensureProcessRunning(ctx)

		case <-ctx.Done():
			s.stopProcess(ctx, cmd, exitCh)
			return
		}
	}
}

func (s *PySupervisor) ensureProcessRunning(ctx context.Context) (*exec.Cmd, chan error) {
	for {
		// The config may have been updated before the process exited.
		// Consume the pending update because the restarted process will
		// already start with the latest config.
		select {
		case <-s.configUpdatedCh:
		default:
		}

		var cmd *exec.Cmd
		var exitCh chan error

		for cmd == nil {
			cmd, exitCh = s.startProcess(ctx)
			if cmd != nil {
				break
			}
			if !s.waitRestart(ctx) {
				return nil, nil
			}
		}

		healthCtx, cancelHealth := context.WithCancel(ctx)
		healthCh := make(chan error, 1)

		go func() {
			healthCh <- s.healthWaiter.Wait(healthCtx)
		}()

		select {
		case err := <-healthCh:
			cancelHealth()
			if err != nil {
				s.logger.Error(ctx,
					"process failed to become healthy",
					log.Any("error", err),
				)

				s.stopProcess(ctx, cmd, exitCh)
				if !s.waitRestart(ctx) {
					return nil, nil
				}
				continue
			}

		case err := <-exitCh:
			cancelHealth()

			s.logProcessExited(ctx, err)

			if !s.waitRestart(ctx) {
				return nil, nil
			}
			continue

		case <-ctx.Done():
			cancelHealth()
			s.stopProcess(ctx, cmd, exitCh)
			return nil, nil
		}

		// healthy
		for module, hosts := range s.modulesHosts {
			err := s.innerRepo.ReceiveModuleAddresses(ctx, module, hosts)
			if err != nil {
				s.logger.Error(ctx, "restore hosts for module",
					log.String("module", module),
					log.Any("error", err),
				)
			}
		}

		return cmd, exitCh
	}
}

func (s *PySupervisor) startProcess(ctx context.Context) (*exec.Cmd, chan error) {
	cmd := exec.Command("uv", "run", s.pyModulePath) // nolint:gosec,noctx

	cmd.Env = append(os.Environ(),
		"BINDING_ADDRESS="+s.bindingAddress,
		"CONFIG_FILE="+s.configPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)

	err := cmd.Start()
	if err != nil {
		s.logger.Error(ctx, "start failed", log.Any("error", err))
		return nil, nil
	}

	s.logger.Info(ctx, "python service started",
		log.String("bindingAddress", s.bindingAddress),
		log.String("configPath", s.configPath),
		log.String("modulePath", s.pyModulePath),
	)

	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	return cmd, exitCh
}

func (s *PySupervisor) logProcessExited(ctx context.Context, err error) {
	if err == nil {
		s.logger.Info(ctx, "process exited gracefully")
		return
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		s.logger.Warn(ctx, "process wait error", log.Any("error", err))
		return
	}

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		s.logger.Warn(ctx, "process exited with unexpected status",
			log.Any("error", err),
		)
		return
	}

	switch {
	case status.Signaled():
		sig := status.Signal()
		s.logger.Info(ctx, "process exited", log.String("signal", sig.String()))
	case status.Exited():
		code := status.ExitStatus()
		s.logger.Info(ctx, "process exited", log.Int("code", code))
	default:
		s.logger.Warn(ctx, "process exited with unexpected status",
			log.Bool("stopped", status.Stopped()),
			log.Bool("continued", status.Continued()),
			log.Any("error", err),
		)
	}
}

func (s *PySupervisor) waitRestart(ctx context.Context) bool {
	timer := time.NewTimer(restartProcessWaitTime)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
