package mq

import (
	"sync"

	"github.com/GMWalletApp/epusdt/config"
)

var (
	startOnce        sync.Once
	callbackLimiter  chan struct{}
	callbackInflight sync.Map
	callbackWorkers  sync.WaitGroup
)

func Start() {
	startOnce.Do(func() {
		callbackLimiter = make(chan struct{}, config.GetQueueConcurrency())
		go runOrderExpirationLoop()
		go runOrderCallbackLoop()
		go runTransactionLockCleanupLoop()
	})
}
