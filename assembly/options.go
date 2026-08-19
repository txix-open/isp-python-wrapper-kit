//nolint:mnd
package assembly

import "time"

type Option func(*options)

type options struct {
	restartProcessWaitTime time.Duration
}

func defaultOptions() options {
	return options{
		restartProcessWaitTime: 1 * time.Second,
	}
}

func WithRestartProcessWaitTime(d time.Duration) Option {
	return func(opts *options) {
		opts.restartProcessWaitTime = d
	}
}
