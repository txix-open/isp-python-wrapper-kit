package service

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/txix-open/isp-kit/log"
)

type HealthRepo interface {
	Healthcheck(ctx context.Context) error
}

type HealthWaiter struct {
	startDelay    time.Duration
	retryInterval time.Duration
	timeout       time.Duration // <= 0 - disabled

	healthRepo HealthRepo
	logger     log.Logger
}

func NewHealthWaiter(
	startDelay time.Duration,
	retryInterval time.Duration,
	timeout time.Duration,
	healthRepo HealthRepo,
	logger log.Logger,
) HealthWaiter {
	return HealthWaiter{
		startDelay:    startDelay,
		retryInterval: retryInterval,
		timeout:       timeout,
		healthRepo:    healthRepo,
		logger:        logger,
	}
}

func (s HealthWaiter) Wait(ctx context.Context) error {
	if s.startDelay > 0 {
		select {
		case <-time.After(s.startDelay):
		case <-ctx.Done():
			return errors.WithMessage(ctx.Err(), "wait start delay")
		}
	}

	var checkCtx context.Context
	var cancel context.CancelFunc

	if s.timeout > 0 {
		checkCtx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	} else {
		checkCtx = ctx
	}

	for {
		err := s.healthRepo.Healthcheck(checkCtx)
		if err == nil {
			return nil
		}
		s.logger.Warn(ctx, "healthcheck failed", log.Any("error", err))

		timer := time.NewTimer(s.retryInterval)
		select {
		case <-timer.C:
		case <-checkCtx.Done():
			timer.Stop()
			return errors.WithMessage(checkCtx.Err(), "healthcheck stopped")
		}
	}
}
