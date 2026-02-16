package main

import (
	"github.com/txix-open/isp-kit/log"
	pythonWrapper "github.com/txix-open/isp-python-wrapper-kit"
)

type remoteConfig struct {
	LogLevel log.Level `schemaGen:"logLevel" schema:"Уровень логирования"`
}

//	@title			isp-python-wrapper-service-template
//	@version		1.0.0
//	@description	Шаблон сервиса-обёртки для Python

//	@license.name	GNU GPL v3.0

//	@host		localhost:9000
//	@BasePath	/api/isp-python-wrapper-service-template

//go:generate swag init -pd -ot json
//go:generate rm -f docs/swagger.go
func main() {
	pythonWrapper.Main[remoteConfig](remoteConfig{}, nil, nil)
}
