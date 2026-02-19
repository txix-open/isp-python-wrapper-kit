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

type Upgrader interface {
	Upgrade(ctx context.Context, moduleName string, hosts []string) error
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

	hostsUpgrader Upgrader
	modulesHosts  map[string][]string

	stopCh          chan bool
	configUpdatedCh chan bool
	upgradeCh       chan upgradeHostsEvent
}

func NewPySupervisor(
	bindingAddress string,
	configPath string,
	pyModulePath string,
	hostsUpgrader Upgrader,
	requiredModules []string,
	logger log.Logger,
) *PySupervisor {
	return &PySupervisor{
		bindingAddress: bindingAddress,
		configPath:     configPath,
		pyModulePath:   pyModulePath,
		hostsUpgrader:  hostsUpgrader,
		logger:         logger,

		modulesHosts:    make(map[string][]string, len(requiredModules)),
		stopCh:          make(chan bool, 1),
		configUpdatedCh: make(chan bool, 1),
		upgradeCh:       make(chan upgradeHostsEvent, len(requiredModules)),
	}
}

func (s *PySupervisor) Start(ctx context.Context) error {
	ctx = log.ToContext(ctx, log.String("worker", "supervisor"))
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

func (s *PySupervisor) Stop() {
	close(s.stopCh)
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

			if cmd != nil {
				s.logger.Info(ctx, "apply hosts upgrade", log.String("module", ev.module))
				_ = s.hostsUpgrader.Upgrade(ctx, ev.module, ev.hosts)
			}

		case <-s.configUpdatedCh:
			if cmd != nil {
				s.stopProcess(ctx, cmd, exitCh)
			}
			cmd, exitCh = s.ensureProcessRunning(ctx)

		case err := <-exitCh:
			s.logger.Warn(ctx, "python exited", log.Any("error", err))
			time.Sleep(restartProcessWaitTime)
			cmd, exitCh = s.ensureProcessRunning(ctx)

		case <-s.stopCh:
			if cmd != nil {
				s.stopProcess(ctx, cmd, exitCh)
			}
			return
		}
	}
}

func (s *PySupervisor) ensureProcessRunning(ctx context.Context) (*exec.Cmd, chan error) {
	var cmd *exec.Cmd
	var exitCh chan error

	for cmd == nil {
		cmd, exitCh = s.startProcess(ctx)
		if cmd == nil {
			time.Sleep(restartProcessWaitTime)
		}
	}

	for module, hosts := range s.modulesHosts {
		err := s.hostsUpgrader.Upgrade(ctx, module, hosts)
		if err != nil {
			s.logger.Error(ctx, "restore hosts for module", log.String("module", module), log.Any("error", err))
		}
	}
	return cmd, exitCh
}

func (s *PySupervisor) startProcess(ctx context.Context) (*exec.Cmd, chan error) {
	cmd := exec.Command("uv", "run", s.pyModulePath) // nolint:gosec,noctx

	cmd.Env = append(os.Environ(),
		"BINDING_ADDRESS="+s.bindingAddress,
		"CONFIG_FILE="+s.configPath,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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

func (s *PySupervisor) stopProcess(
	ctx context.Context,
	cmd *exec.Cmd,
	done chan error,
) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	err := cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		s.logger.Warn(ctx, "failed to send SIGTERM", log.Any("error", err))
	}

	select {
	case err = <-done:
	case <-time.After(shutdownProcessTimeout):
		err = cmd.Process.Kill()
	}

	if err != nil {
		s.logger.Warn(ctx, "python process did not stop gracefully", log.Any("error", err))
	}
}
