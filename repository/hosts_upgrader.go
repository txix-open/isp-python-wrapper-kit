package repository

import (
	"context"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/http/httpcli"
)

const (
	receiveModuleAddressEndpoint = "/receive_module_addresses"
)

type HostsUpgrader struct {
	innerCli *httpcli.Client
}

func NewHostsUpgrader(
	innerCli *httpcli.Client,
) HostsUpgrader {
	return HostsUpgrader{
		innerCli: innerCli,
	}
}

func (r HostsUpgrader) Upgrade(ctx context.Context, moduleName string, hosts []string) error {
	payload := map[string]any{
		"module": moduleName,
		"hosts":  hosts,
	}
	err := r.innerCli.Post(receiveModuleAddressEndpoint).
		JsonRequestBody(payload).
		StatusCodeToError().
		DoWithoutResponse(ctx)

	if err != nil {
		return errors.WithMessagef(err, "call endpoint: %s", receiveModuleAddressEndpoint)
	}
	return nil
}
