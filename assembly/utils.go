package assembly

import (
	"os"
	"path"
	"strings"

	"github.com/pkg/errors"
)

func isOnDevMode() bool {
	return strings.ToLower(os.Getenv("APP_MODE")) == "dev"
}

func resolvePyModulePath(isDev bool) (string, error) {
	cfgPath := os.Getenv("APP_PYTHON_PATH")
	if cfgPath != "" {
		return cfgPath, nil
	}

	if isDev {
		return "./main.py", nil
	}

	return relativePathFromBin("main.py")
}

func resolveConfigPath(isDev bool) (string, error) {
	cfgPath := os.Getenv("APP_PYTHON_CONFIG_PATH")
	if cfgPath != "" {
		return cfgPath, nil
	}

	if isDev {
		return "./config.json", nil
	}

	return relativePathFromBin("config.json")
}

func relativePathFromBin(part string) (string, error) {
	ex, err := os.Executable()
	if err != nil {
		return "", errors.WithMessage(err, "get executable path")
	}
	return path.Join(path.Dir(ex), part), nil
}
