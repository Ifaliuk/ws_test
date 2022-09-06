package gracefull_shutdown

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"
)

type gsCallback struct {
	name     string
	callback func(ctx context.Context) error
}

type GracefulShutdown struct {
	ctx              context.Context
	cancel           context.CancelFunc
	terminateSignals map[os.Signal]struct{}
	otherSignals     map[os.Signal]func(ctx context.Context) error
	waitCallbacks    []*gsCallback
}

func NewGracefulShutdown(terminateSignals ...os.Signal) *GracefulShutdown {
	ctx, cancel := context.WithCancel(context.Background())

	terminateSignalsMap := make(map[os.Signal]struct{})
	for _, terminateSignal := range terminateSignals {
		terminateSignalsMap[terminateSignal] = struct{}{}
	}

	return &GracefulShutdown{
		ctx:              ctx,
		cancel:           cancel,
		terminateSignals: terminateSignalsMap,
		otherSignals:     make(map[os.Signal]func(ctx context.Context) error),
		waitCallbacks:    make([]*gsCallback, 0, 100),
	}
}

func (gs *GracefulShutdown) GetContext() context.Context {
	return gs.ctx
}

func (gs *GracefulShutdown) CancelContext() {
	gs.cancel()
}

func (gs *GracefulShutdown) AddWaitCallback(name string, callback func(ctx context.Context) error) {
	gs.waitCallbacks = append(gs.waitCallbacks, &gsCallback{
		name:     name,
		callback: callback,
	})
}

func (gs *GracefulShutdown) AddListenSignal(signal os.Signal, callback func(ctx context.Context) error) {
	gs.otherSignals[signal] = callback
}

func (gs *GracefulShutdown) Wait(timeout time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		signals := make([]os.Signal, len(gs.terminateSignals)+len(gs.otherSignals))

		var (
			sign os.Signal
			err  error
		)

		const prefix = "Graceful Shutdown"

		for sign = range gs.terminateSignals {
			signals = append(signals, sign)
		}
		for sign = range gs.otherSignals {
			signals = append(signals, sign)
		}
		signCh := make(chan os.Signal, 1)
		signal.Notify(signCh, signals...)

	LOOP:
		for {
			select {
			case sign = <-signCh:
				if _, ok := gs.terminateSignals[sign]; ok {
					log.Printf("[%s] Received %v signal from OS", prefix, sign)
					gs.cancel()
					break LOOP
				}
				if otherCallback, ok := gs.otherSignals[sign]; ok {
					if err = otherCallback(gs.ctx); err != nil {
						log.Printf("[%s] Can't run callback for signal %v: %v", prefix, sign, err)
					}
				}
			case <-gs.ctx.Done():
				break LOOP
			}
		}

		forceTimer := time.AfterFunc(timeout, func() {
			log.Printf("[%s] Timeout %d seconds has been elapsed, force exit", prefix, timeout.Seconds())
			os.Exit(0)
		})
		defer forceTimer.Stop()

		wg := sync.WaitGroup{}

		for _, item := range gs.waitCallbacks {
			wg.Add(1)
			go func(cb *gsCallback) {
				defer wg.Done()
				log.Printf("[%s] Cleaning up: %s: start...", prefix, cb.name)
				if err := cb.callback(gs.ctx); err != nil {
					log.Printf("[%s] Cleaning up: %s: failed: %s", prefix, cb.name, err.Error())
					return
				}
				log.Printf("[%s] Cleaning up: %s: finish...", prefix, cb.name)
			}(item)
		}

		wg.Wait()

	}()
	return done
}
