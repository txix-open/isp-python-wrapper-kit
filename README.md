# isp-python-wrapper-kit
## Назначение библиотеки
`isp-python-wrapper-kit` — это библиотека, которая:

* Запускает Python-модуль как дочерний процесс из Go
* Передаёт ему `json` (`config.json`) конфигурацию через файл и env-переменные (`BINDING_ADDRESS` и `CONFIG_FILE`)
* Перезапускает Python-процесс при изменении конфигурации
* Интегрируется с инфраструктурой
* Управляет жизненным циклом Python-сервиса как частью Go-приложения

## Жизненный цикл дочернего процесса
Python-процесс управляется компонентом PySupervisor и существует в рамках жизненного цикла Go-приложения.

Основные состояния:

1) Idle (ожидание конфигурации)
2) Starting (запуск процесса) - запуск через `uv run`, ожидание 200-299 на `GET /internal/health`
3) Running (процесс healthy и работает)
4) Restarting:
	1) перезапуск при получении нового конфига: выключение дочернего процесса и повторный запуск
	2) Timeout на healthcheck при запуске
	3) при самостоятельной остановке дочернего процесса: ожидание `1s` и повторный запуск
5) Stopping (остановка) - передача дочернему процессу сигнала `SIGTERM` с таймаутом 5s, если за это время процесс не останавливается, вызывается `kill` дочернего процесса. В unix сигналы отправляются всей `pgid`
6) Stopped (завершён)

## Конфигурация
### HealthCheck

В `config.yml` можно указать настройки для healthcheck'а (настройка `python`):
* `healthcheckStartDelay` - время, через которое будет запущена проверка `healthcheck`, по умолчанию `100ms`
* `healthcheckRetryDelay` - время, через которое будет повтор проверки `healthcheck`, если процесс ответил не 200-299 статус кодом (или не ответил вообще), по умолчанию `1s`
* `healthcheckTimeout` - время, через которое при отсутствии успешного ответа процесс будет считаться неисправным, по умолчанию отключен (`0s`) 

Пример настройки:
```yml
python:
  healthcheckStartDelay: 10s
  healthcheckRetryDelay: 5s
  healthcheckTimeout: 5s
```

## Требования

1) `main.py`, `pyproject.toml` и `uv.lock` должны находиться в корне проекта
2) Для получения адресов `required` сервисов необходимо, чтобы сервис на питоне реализовывал метод `POST /receive_module_addresses` (запрос направляется после перехода сервиса в состояние `healthy`) и принимал тела вида:
```json
{
    "module": "<moduleName>",
	"hosts":  ["<host:port>","<host:port>"],
}
```
3) В директории `conf` должны лежать `config.yml` и `default_remote_config.json`
4) Сервис должен реализовать метод `GET /internal/health`

## Использование
### Сервис без endpoint'ов
```go
package main

import (
	"my-module/conf"
	wrapperkit "gitlab.txix.ru/isp/isp-python-wrapper-kit"
)

var (
	version = "1.0.0"
)

func main() {
	wrapperKit.Main[conf.Remote](version, conf.Remote{}, nil, nil)
}
```

### Сервис с endpoint'ами
```go
package main

import (
	"my-module/conf"
    "my-module/routes"
	wrapperkit "gitlab.txix.ru/isp/isp-python-wrapper-kit"
)

var (
	version = "1.0.0"
)

func main() {
	wrapperKit.Main[conf.Remote](version, conf.Remote{}, routes.EndpointDescriptors(), nil)
}
```

### Сервис с required модулями
```go
package main

import (
	"my-module/conf"
	wrapperkit "gitlab.txix.ru/isp/isp-python-wrapper-kit"
)

var (
	version = "1.0.0"
)

func main() {
	wrapperKit.Main[conf.Remote](version, conf.Remote{}, nil, []string{"<required-module-name1>","<required-module-name2>"})
}
```