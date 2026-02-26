package assembly

import (
	"context"
	"fmt"

	"github.com/txix-open/isp-python-wrapper-kit/repository"
	"github.com/txix-open/isp-python-wrapper-kit/service"

	"github.com/tidwall/gjson"
	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/http/httpclix"
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
	Upgrade(moduleName string, hosts []string)
	Close() error
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
	innerCli := httpclix.Default(
		httpcli.WithMiddlewares(httpclix.Log(logger)),
	)
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
	upgrader := repository.NewHostsUpgrader(innerCli)

	pySupervisor := service.NewPySupervisor(
		boot.BindingAddress,
		configPath,
		pythonModulePath,
		upgrader,
		requiredModules,
		logger,
	)
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

	cfgLogLevel := gjson.GetBytes(remoteConfig, "logLevel").String()

	logLevel := log.Level(0)
	err = logLevel.UnmarshalText([]byte(cfgLogLevel))
	if err != nil {
		a.boot.Fatal(errors.WithMessage(err, "parse log level"))
	}
	a.logger.SetLevel(logLevel)

	err = a.pySupervisor.UpdateConfig(remoteConfig)
	if err != nil {
		a.boot.Fatal(errors.WithMessage(err, "update config"))
	}

	return nil
}

func (a *Assembly[T]) Runners() []app.Runner {
	eventHandler := cluster.NewEventHandler().
		RemoteConfigReceiver(a)

	for _, requiredModule := range a.requiredModules {
		eventHandler = eventHandler.RequireModule(
			requiredModule,
			service.NewHostsUpgrader(requiredModule, a.pySupervisor.Upgrade),
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
		a.pySupervisor,
	}
}
