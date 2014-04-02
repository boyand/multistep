package multistep

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type runState int32

const (
	stateIdle runState = iota
	stateRunning
	stateCancelling
)

// BasicRunner is a Runner that just runs the given slice of steps.
type BasicRunner struct {
	// Steps is a slice of steps to run. Once set, this should _not_ be
	// modified.
	Steps []Step

	cancelCh chan struct{}
	doneCh   chan struct{}
	state    runState
	sync.Mutex
}

//NewRunner returns a new BasicRunner instance

func NewBasicRunner(steps []Step) *BasicRunner {
	return &BasicRunner{
		Steps:    steps,
		cancelCh: make(chan struct{}),
		doneCh:   make(chan struct{}),
		state:    stateIdle,
	}
}

func (b *BasicRunner) Run(state StateBag) error {
	b.Lock()

	if b.state != stateIdle {
		return fmt.Errorf("Already running")
	}

	b.state = stateRunning
	b.Unlock()

	defer func() {
		b.Lock()
		//b.cancelCh = nil
		//b.doneCh = nil
		b.state = stateIdle
		close(b.doneCh)
		//close(b.cancelCh)
		b.Unlock()
	}()

	// This goroutine listens for cancels and puts the StateCancelled key
	// as quickly as possible into the state bag to mark it.
	go func() {
		select {
		case <-b.cancelCh:
			// Flag cancel and wait for finish
			state.Put(StateCancelled, true)
			<-b.doneCh
		case <-b.doneCh:
		}
	}()

	for _, step := range b.Steps {
		// We also check for cancellation here since we can't be sure
		// the goroutine that is running to set it actually ran.
		if runState(atomic.LoadInt32((*int32)(&b.state))) == stateCancelling {
			state.Put(StateCancelled, true)
			break
		}

		action := step.Run(state)
		defer step.Cleanup(state)

		if _, ok := state.GetOk(StateCancelled); ok {
			break
		}

		if action == ActionHalt {
			state.Put(StateHalted, true)
			break
		}
	}
	return nil
}

func (b *BasicRunner) Cancel() {
	b.Lock()
	switch b.state {
	case stateIdle:
		// Not running, so Cancel is... done.
		b.Unlock()
		return
	case stateRunning:
		// Running, so mark that we cancelled and set the state
		close(b.cancelCh)
		b.state = stateCancelling
		fallthrough
	case stateCancelling:
		// Already cancelling, so just wait until we're done
		//ch := b.doneCh
		b.Unlock()
		<-b.doneCh
	}
}
