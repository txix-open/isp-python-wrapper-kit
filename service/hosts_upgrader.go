package service

import (
	"context"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/log"
)

type Upgrader interface {
	Upgrade(ctx context.Context, moduleName string, hosts []string) error
}

type HostsUpgrader struct {
	moduleName string
	upgrader   Upgrader
	logger     log.Logger
}

func NewHostsUpgrader(
	moduleName string,
	upgrader Upgrader,
	logger log.Logger,
) HostsUpgrader {
	return HostsUpgrader{
		moduleName: moduleName,
		upgrader:   upgrader,
		logger:     logger,
	}
}

func (s HostsUpgrader) Upgrade(hosts []string) {
	ctx := context.Background()
	err := s.upgrader.Upgrade(ctx, s.moduleName, hosts)
	if err != nil {
		s.logger.Error(ctx,
			errors.WithMessage(err, "upgrade hosts for module"),
			log.String("moduleName", s.moduleName),
		)
	}
}
