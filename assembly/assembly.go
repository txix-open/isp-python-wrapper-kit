package assembly

import (
	"context"
	"fmt"

	"gitlab.txix.ru/isp/isp-python-wrapper-kit/repository"
	"gitlab.txix.ru/isp/isp-python-wrapper-kit/service"

	"github.com/tidwall/gjson"
	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/rc"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/app"
	"github.com/txix-open/isp-kit/bootstrap"
	"github.com/txix-open/isp-kit/cluster"
	"github.com/txix-open/isp-kit/log"
)

type PythonSupervisor interface {
	Start(ctx context.Context) error
	UpdateConfig(newConfig []byte) error
	Stop()
}

type Assembly[T any] struct {
	boot            *bootstrap.Bootstrap
	innerCli        *httpcli.Client
	requiredModules []string
	pySupervisor    PythonSupervisor
	logger          *log.Adapter
}

func New[T any](boot *bootstrap.Bootstrap, requiredModules []string) (*Assembly[T], error) {
	logger := boot.App.Logger()
	innerCli := httpcli.New()
	innerCli.GlobalRequestConfig().BaseUrl = fmt.Sprintf("http://%s", boot.BindingAddress)

	isOnDev := isOnDevMode()
	configPath, err := resolveConfigPath(isOnDev)
	if err != nil {
		return nil, errors.WithMessage(err, "resolve config path")
	}

	pythonModulePath, err := resolvePyModulePath(isOnDev)
	if err != nil {
		return nil, errors.WithMessage(err, "resolve python module path")
	}
	pySupervisor := service.NewPySupervisor(boot.BindingAddress, configPath, pythonModulePath, logger)

	return &Assembly[T]{
		boot:            boot,
		innerCli:        innerCli,
		requiredModules: requiredModules,
		pySupervisor:    pySupervisor,
		logger:          logger,
	}, nil
}

func (a *Assembly[T]) ReceiveConfig(shortTtlCtx context.Context, remoteConfig []byte) error {
	_, _, err := rc.Upgrade[T](a.boot.RemoteConfig, remoteConfig)
	if err != nil {
		a.boot.Fatal(errors.WithMessage(err, "upgrade remote config"))
	}
	// nolint:gosec
	a.logger.SetLevel(log.Level(gjson.GetBytes(remoteConfig, "logLevel").Int()))

	err = a.pySupervisor.UpdateConfig(remoteConfig)
	if err != nil {
		a.boot.Fatal(errors.WithMessage(err, "update config"))
	}

	return nil
}

func (a *Assembly[T]) Runners() []app.Runner {
	eventHandler := cluster.NewEventHandler().
		RemoteConfigReceiver(a)

	upgrader := repository.NewHostsUpgrader(a.innerCli)
	for _, requiredModule := range a.requiredModules {
		eventHandler = eventHandler.RequireModule(
			requiredModule,
			service.NewHostsUpgrader(requiredModule, upgrader, a.logger),
		)
	}

	return []app.Runner{
		app.RunnerFunc(func(ctx context.Context) error {
			err := a.boot.ClusterCli.Run(ctx, eventHandler)
			if err != nil {
				return errors.WithMessage(err, "run cluster client")
			}
			return nil
		}),
		app.RunnerFunc(func(ctx context.Context) error {
			return a.pySupervisor.Start(ctx)
		}),
	}
}

func (a *Assembly[T]) Closers() []app.Closer {
	return []app.Closer{
		a.boot.ClusterCli,
		app.CloserFunc(func() error {
			a.pySupervisor.Stop()
			return nil
		}),
	}
}
