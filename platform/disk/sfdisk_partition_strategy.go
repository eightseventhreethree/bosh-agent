package disk

import (
	"time"

	"code.cloudfoundry.org/clock"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshretry "github.com/cloudfoundry/bosh-utils/retrystrategy"
)

type partitionStrategy struct {
	retryable   boshretry.Retryable
	timeService clock.Clock
	logger      boshlog.Logger
}

func NewPartitionStrategy(
	retryable boshretry.Retryable,
	timeService clock.Clock,
	logger boshlog.Logger,
) boshretry.RetryStrategy {
	return &partitionStrategy{
		retryable:   retryable,
		logger:      logger,
		timeService: timeService,
	}
}

func (s *partitionStrategy) Try() error {
	var err error
	var shouldRetry bool

	for i := 0; i < 20; i++ {
		s.logger.Debug("attemptRetryStrategy", "Making attempt #%d", i)

		shouldRetry, err = s.retryable.Attempt()
		if !shouldRetry {
			return err
		}

		s.timeService.Sleep(3 * time.Second)
	}

	return err
}
