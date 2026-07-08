// This package manages the lifecycle of a long-lived connection that
// must be re-established whenever it drops.
//
// Contracts between the reconnector and its callbacks:
//
//   - Close must cause a blocked Watch to return.
//     Watch does not need to observe any stop signal of its own.
//
//   - Watch reports a failure with a single non-blocking send to errChannel
//     and then returns. The channel belongs to a single connection
//     generation, so a slow Watch can never tear down a newer connection.
//     The send must be non-blocking because Reconnect
//     shares the channel and may have already filled its buffer.
//
//   - If Dial needs to remember the configuration it dialed with (for change
//     detection on config reload), it must embed that snapshot in C so it
//     travels with the connection it produced, rather than the client
//     re-reading config later and comparing against a moving target.
package reconnect

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// DefaultRetryDelay is the fixed delay between attempts
const DefaultRetryDelay = 2 * time.Second

// Options configures a Reconnector.
type Options[C any] struct {
	Logger *zap.SugaredLogger

	// Enabled gates connection attempts.
	// When nil, the reconnector is always on.
	Enabled func() bool

	// Dial establishes a new connection. It may block and is not cancellable;
	// the reconnector calls it from its own goroutine so that Stop never
	// waits for a dial in flight. It must be free of side effects on shared
	// state - the reconnector decides whether the result is adopted.
	Dial func() (C, error)

	// Watch runs for the lifetime of a single connection and returns when
	// that connection is finished. When the connection fails on its own,
	// Watch sends one error to errChannel before returning; when it returns
	// because Close unblocked it, no send is needed.
	Watch func(conn C, errChannel chan<- error)

	// Close releases a connection. It must unblock a Watch that is blocked
	// on the connection. It is called at most once per connection.
	Close func(conn C)

	// OnUp is called after a connection is adopted, before Watch starts.
	OnUp func(conn C)

	// OnDown is called when an established connection is lost (or torn down
	// via Reconnect), before Close. It is not called for failed dials.
	OnDown func(err error)

	// Backoff returns the delay before the next dial, given the number of
	// consecutive failures so far (0 for the first retry after a failure,
	// increasing while failures continue, reset when a connection is
	// adopted). When nil, a fixed DefaultRetryDelay is used.
	Backoff func(attempt int) time.Duration
}

// Reconnector maintains at most one live connection of type C, redialing
// with backoff whenever the connection drops.
//
// Start, Stop and the Reconnector's construction must happen on one
// goroutine (or be otherwise serialized); all other methods are safe to call
// from any goroutine.
type Reconnector[C any] struct {
	opts   Options[C]
	logger *zap.SugaredLogger

	// current, hasConn and errChannel describe the current connection
	// generation and are replaced together under lock on every (re)connect
	lock       sync.Mutex
	current    C
	hasConn    bool
	errChannel chan error

	stopChannel chan struct{}
	wg          sync.WaitGroup
}

// New creates a Reconnector from opts.
// Dial, Watch or Close are required
func New[C any](opts Options[C]) *Reconnector[C] {
	if opts.Dial == nil || opts.Watch == nil || opts.Close == nil {
		panic("reconnect: Options.Dial, Options.Watch and Options.Close are required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	return &Reconnector[C]{
		opts:   opts,
		logger: logger,
	}
}

// Start launches the manager loop.
func (r *Reconnector[C]) Start() {
	stopChannel := make(chan struct{})
	r.stopChannel = stopChannel

	r.wg.Go(func() {
		r.managerLoop(stopChannel)
	})
}

// Stop tears down the current connection (if any) and waits for the manager
// loop and its Watch to exit. It never waits for a dial in flight.
func (r *Reconnector[C]) Stop() {
	if r.stopChannel == nil {
		return
	}

	close(r.stopChannel)
	r.wg.Wait()
	r.stopChannel = nil
}

// Reconnect asks the manager loop to tear down the current connection and
// dial again, reporting reason to OnDown.
func (r *Reconnector[C]) Reconnect(reason error) {
	r.lock.Lock()
	errChannel := r.errChannel
	r.lock.Unlock()

	if errChannel == nil {
		return
	}

	select {
	case errChannel <- reason:
	default:
		// channel full, teardown already pending
	}
}

// Current returns the live connection, or false when disconnected
func (r *Reconnector[C]) Current() (C, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.current, r.hasConn
}

// Connected reports whether a connection is currently established.
func (r *Reconnector[C]) Connected() bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.hasConn
}

func (r *Reconnector[C]) backoffDelay(attempt int) time.Duration {
	if r.opts.Backoff == nil {
		return DefaultRetryDelay
	}

	return r.opts.Backoff(attempt)
}

// sleeps for delay, returning false early
// if stopChannel closes first.
func (r *Reconnector[C]) waitOrStop(stopChannel <-chan struct{}, delay time.Duration) bool {
	select {
	case <-stopChannel:
		return false
	case <-time.After(delay):
		return true
	}
}

// drop clears the current connection generation and closes its connection.
func (r *Reconnector[C]) drop() {
	r.lock.Lock()

	if !r.hasConn {
		r.lock.Unlock()
		return
	}

	conn := r.current

	var zero C
	r.current = zero
	r.hasConn = false
	r.errChannel = nil

	r.lock.Unlock()

	// close outside the lock: Close may block until Watch unblocks, and
	// request paths calling Current must not stall behind it
	r.opts.Close(conn)
}

type dialResult[C any] struct {
	conn C
	err  error
}

func (r *Reconnector[C]) managerLoop(stopChannel <-chan struct{}) {
	// consecutive failures since the last adopted connection
	attempt := 0

	for {
		if r.opts.Enabled != nil && !r.opts.Enabled() {
			if !r.waitOrStop(stopChannel, r.backoffDelay(0)) {
				r.logger.Debug("managerLoop: stop signal")
				return
			}

			continue
		}

		// dial in a goroutine so we can respond to the stop signal
		dialDone := make(chan dialResult[C], 1)
		go func() {
			conn, err := r.opts.Dial()
			dialDone <- dialResult[C]{conn: conn, err: err}
		}()

		var result dialResult[C]

		select {
		case <-stopChannel:
			r.logger.Debug("managerLoop: stop signal during dial")

			// let it resolve in the background and close the connection if
			// it ends up succeeding.
			go func() {
				if late := <-dialDone; late.err == nil {
					r.opts.Close(late.conn)
				}
			}()
			return

		case result = <-dialDone:
		}

		if result.err != nil {
			r.logger.Debugw("Dial failed, retrying...", "error", result.err, "attempt", attempt)

			if !r.waitOrStop(stopChannel, r.backoffDelay(attempt)) {
				r.logger.Debug("managerLoop: stop signal")
				return
			}

			attempt++
			continue
		}

		// if the connector was turned off while dialing,
		// drop the connection
		if r.opts.Enabled != nil && !r.opts.Enabled() {
			r.logger.Debug("Disabled while dialing, dropping connection")
			r.opts.Close(result.conn)
			continue
		}

		// publish connection together with a fresh error
		// channel for this connection generation
		errChannel := make(chan error, 1)

		r.lock.Lock()
		r.current = result.conn
		r.hasConn = true
		r.errChannel = errChannel
		r.lock.Unlock()

		attempt = 0

		r.logger.Debug("Connection established")

		if r.opts.OnUp != nil {
			r.opts.OnUp(result.conn)
		}

		// watch this connection to detect disconnection. it receives this
		// generation's connection and error channel
		r.wg.Go(func() {
			r.opts.Watch(result.conn, errChannel)
		})

		select {
		case <-stopChannel:
			r.logger.Debug("managerLoop: stop signal")
			r.drop()
			return

		case err := <-errChannel:
			r.logger.Debugw("Connection lost, reconnecting...", "error", err)

			if r.opts.OnDown != nil {
				r.opts.OnDown(err)
			}

			r.drop()

			if !r.waitOrStop(stopChannel, r.backoffDelay(attempt)) {
				r.logger.Debug("managerLoop: stop signal")
				return
			}

			attempt++
		}
	}
}
