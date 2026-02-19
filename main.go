package isp_python_wrapper_kit

import (
	"github.com/txix-open/isp-python-wrapper-kit/assembly"

	"github.com/txix-open/isp-kit/bootstrap"
	"github.com/txix-open/isp-kit/cluster"
	"github.com/txix-open/isp-kit/shutdown"
)

var (
	version = "1.0.0"
)

func Main[T any](
	remoteConfig any,
	endpoints []cluster.EndpointDescriptor,
	requiredModules []string,
) {
	boot := bootstrap.New(
		version,
		remoteConfig,
		endpoints,
		cluster.HttpTransport,
	)
	app := boot.App
	logger := app.Logger()

	assembly, err := assembly.New[T](boot, requiredModules)
	if err != nil {
		boot.Fatal(err)
	}
	app.AddRunners(assembly.Runners()...)
	app.AddClosers(assembly.Closers()...)

	shutdown.On(func() {
		logger.Info(app.Context(), "starting shutdown")
		app.Shutdown()
		logger.Info(app.Context(), "shutdown completed")
	})

	err = app.Run()
	if err != nil {
		boot.Fatal(err)
	}
}
