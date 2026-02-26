package repository

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/retry"
)

const (
	receiveModuleAddressEndpoint = "/receive_module_addresses"
	maxRetryElapsedTime          = 5 * time.Second
)

type Inner struct {
	innerCli *httpcli.Client
}

func NewInner(
	innerCli *httpcli.Client,
) Inner {
	return Inner{
		innerCli: innerCli,
	}
}

func (r Inner) ReceiveModuleAddresses(ctx context.Context, moduleName string, hosts []string) error {
	payload := map[string]any{
		"module": moduleName,
		"hosts":  hosts,
	}
	err := r.innerCli.Post(receiveModuleAddressEndpoint).
		JsonRequestBody(payload).
		Retry(r.shouldRetry, retry.NewExponentialBackoff(maxRetryElapsedTime)).
		StatusCodeToError().
		DoWithoutResponse(ctx)

	if err != nil {
		return errors.WithMessagef(err, "call endpoint: %s", receiveModuleAddressEndpoint)
	}
	return nil
}

func (r Inner) shouldRetry(err error, resp *httpcli.Response) error {
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return errors.Errorf("status code %d", resp.Raw.StatusCode)
	}

	return nil
}
