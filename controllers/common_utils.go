package controllers

import "time"

type RateLimiter struct {
	Burst           int
	Frequency       int
	BaseDelay       time.Duration
	FailureMaxDelay time.Duration
}

const (
	requeueInterval = time.Second * 3
	finalizer       = "sample.kyma-project.io/finalizer"
	debugLogLevel   = 2
	fieldOwner      = "sample.kyma-project.io/owner"
)
