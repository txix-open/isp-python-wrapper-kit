package assembly

import (
	"context"
	"fmt"
	"time"

	"github.com/txix-open/isp-python-wrapper-kit/repository"
	"github.com/txix-open/isp-python-wrapper-kit/service"

	"github.com/tidwall/gjson"
	"github.com/txix-open/isp-kit/config"
	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/http/httpclix"
	"github.com/txix-open/isp-kit/rc"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/app"
	"github.com/txix-open/isp-kit/bootstrap"
	"github.com/txix-open/isp-kit/cluster"
	"github.com/txix-open/isp-kit/log"
)

const (
	defaultHealthcheckStartDelay = 100 * time.Millisecond
	defaultHealthcheckRetryDelay = 1 * time.Second
	defaultHealthcheckTimeout    = 0 * time.Second // выключен
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

type AssemblyConfig struct {
	ConfigPath       string
	PythonModulePath string

	HealthcheckStartDelay time.Duration
	HealthcheckRetryDelay time.Duration
	HealthcheckTimeout    time.Duration
}

func New[T any](
	boot *bootstrap.Bootstrap,
	requiredModules []string,
	opts ...Option,
) (*Assembly[T], error) {
	options := defaultOptions()

	for _, opt := range opts {
		opt(&options)
	}

	logger := boot.App.Logger()
	innerCli := httpclix.Default(
		httpcli.WithMiddlewares(httpclix.Log(logger)),
	)
	innerCli.GlobalRequestConfig().BaseUrl = fmt.Sprintf("http://%s", boot.BindingAddress)

	cfg, err := getConfig(boot.App.Config())
	if err != nil {
		return nil, errors.WithMessage(err, "get config")
	}

	innerRepo := repository.NewInner(innerCli)

	healthWaiter := service.NewHealthWaiter(
		cfg.HealthcheckStartDelay,
		cfg.HealthcheckRetryDelay,
		cfg.HealthcheckTimeout,
		innerRepo,
		logger,
	)

	pySupervisor := service.NewPySupervisor(
		boot.BindingAddress,
		cfg.ConfigPath,
		cfg.PythonModulePath,
		innerRepo,
		requiredModules,
		healthWaiter,
		options.restartProcessWaitTime,
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

func getConfig(cfg *config.Config) (*AssemblyConfig, error) {
	isOnDev := isOnDevMode()
	configPath, err := resolveConfigPath(isOnDev)
	if err != nil {
		return nil, errors.WithMessage(err, "resolve config path")
	}

	pythonModulePath, err := resolvePyModulePath(isOnDev)
	if err != nil {
		return nil, errors.WithMessage(err, "resolve python module path")
	}

	return &AssemblyConfig{
		ConfigPath:            configPath,
		PythonModulePath:      pythonModulePath,
		HealthcheckStartDelay: cfg.Optional().Duration("python.healthcheckStartDelay", defaultHealthcheckStartDelay),
		HealthcheckRetryDelay: cfg.Optional().Duration("python.healthcheckRetryDelay", defaultHealthcheckRetryDelay),
		HealthcheckTimeout:    cfg.Optional().Duration("python.healthcheckTimeout", defaultHealthcheckTimeout),
	}, nil
}
