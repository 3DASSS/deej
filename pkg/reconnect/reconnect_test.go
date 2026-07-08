package reconnect

import (
	"errors"
	"testing"
	"time"
)

const testTimeout = 5 * time.Second

// testConn is the fake connection type driven by the fakeConnector.
// done is closed by Close, which unblocks the fakeConnector's Watch
type testConn struct {
	id   int
	done chan struct{}
}

type watchHandle struct {
	conn       testConn
	errChannel chan<- error
}

type connectResult struct {
	err error
}

// fakeConnector scripts a Reconnector
type fakeConnector struct {
	r *Reconnector[testConn]

	fakeConnRes chan connectResult
	dialStarted chan struct{}
	watches     chan watchHandle
	ups         chan int
	downs       chan error
	closes      chan int
	backoffs    chan int

	enabled chan bool // when non-nil, holds the gate's current value
}

func newFakeConnector(backoff func(int) time.Duration, gated bool) *fakeConnector {
	f := &fakeConnector{
		fakeConnRes: make(chan connectResult, 16),
		dialStarted: make(chan struct{}, 16),
		watches:     make(chan watchHandle, 16),
		ups:         make(chan int, 16),
		downs:       make(chan error, 16),
		closes:      make(chan int, 16),
		backoffs:    make(chan int, 64),
	}

	opts := Options[testConn]{
		Dial:    f.dial,
		Watch:   f.watch,
		Close:   f.close,
		OnUp:    func(c testConn) { f.ups <- c.id },
		OnDown:  func(err error) { f.downs <- err },
		Backoff: nil,
	}

	if backoff != nil {
		opts.Backoff = func(attempt int) time.Duration {
			select {
			case f.backoffs <- attempt:
			default:
			}
			return backoff(attempt)
		}
	}

	if gated {
		f.enabled = make(chan bool, 1)
		f.enabled <- false
		opts.Enabled = func() bool {
			v := <-f.enabled
			f.enabled <- v
			return v
		}
	}

	f.r = New(opts)
	return f
}

func (f *fakeConnector) setEnabled(v bool) {
	<-f.enabled
	f.enabled <- v
}

var nextConnID = make(chan int)

func init() {
	go func() {
		for id := 1; ; id++ {
			nextConnID <- id
		}
	}()
}

func (f *fakeConnector) dial() (testConn, error) {
	f.dialStarted <- struct{}{}

	outcome := <-f.fakeConnRes
	if outcome.err != nil {
		return testConn{}, outcome.err
	}

	return testConn{id: <-nextConnID, done: make(chan struct{})}, nil
}

func (f *fakeConnector) watch(conn testConn, errChannel chan<- error) {
	f.watches <- watchHandle{conn: conn, errChannel: errChannel}
	<-conn.done
}

func (f *fakeConnector) close(conn testConn) {
	close(conn.done)
	f.closes <- conn.id
}

func expectRecv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(testTimeout):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

func expectNone[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()

	select {
	case v := <-ch:
		t.Fatalf("unexpected %s: %v", what, v)
	case <-time.After(150 * time.Millisecond):
	}
}

// stopWithDeadline fails the test if Stop blocks.
func stopWithDeadline(t *testing.T, r *Reconnector[testConn]) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		r.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Stop did not return")
	}
}

func fastBackoff(int) time.Duration { return time.Millisecond }

func TestStopDuringDial(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	// no scripted outcome yet, so the dial stays in flight
	f.r.Start()
	expectRecv(t, f.dialStarted, "dial start")

	// Stop must not wait for the un-cancellable dial
	stopWithDeadline(t, f.r)

	// when the dial eventually succeeds, the never-adopted connection must
	// be closed by the detached cleanup
	f.fakeConnRes <- connectResult{}
	expectRecv(t, f.closes, "close of never-adopted connection")
	expectNone(t, f.ups, "OnUp")
}

func TestStopDuringRetryWait(t *testing.T) {
	f := newFakeConnector(func(int) time.Duration { return time.Hour }, false)

	f.fakeConnRes <- connectResult{err: errors.New("dial failed")}
	f.r.Start()

	// wait until the loop is inside its hour-long retry wait, then make
	// sure Stop interrupts it
	expectRecv(t, f.backoffs, "backoff call")
	time.Sleep(50 * time.Millisecond)

	stopWithDeadline(t, f.r)
}

func TestDialFailRetrySuccess(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	f.fakeConnRes <- connectResult{err: errors.New("fail 1")}
	f.fakeConnRes <- connectResult{err: errors.New("fail 2")}
	f.fakeConnRes <- connectResult{}

	f.r.Start()
	defer stopWithDeadline(t, f.r)

	upID := expectRecv(t, f.ups, "OnUp")

	if !f.r.Connected() {
		t.Error("Connected() = false after OnUp")
	}
	if conn, ok := f.r.Current(); !ok || conn.id != upID {
		t.Errorf("Current() = (%v, %v), expected id %d", conn.id, ok, upID)
	}

	// consecutive dial failures must see increasing attempt counts
	if a := expectRecv(t, f.backoffs, "first backoff"); a != 0 {
		t.Errorf("first backoff attempt = %d, expected 0", a)
	}
	if a := expectRecv(t, f.backoffs, "second backoff"); a != 1 {
		t.Errorf("second backoff attempt = %d, expected 1", a)
	}
}

func TestWatchErrorTriggersReconnect(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	f.fakeConnRes <- connectResult{}
	f.fakeConnRes <- connectResult{}

	f.r.Start()
	defer stopWithDeadline(t, f.r)

	firstID := expectRecv(t, f.ups, "first OnUp")
	handle := expectRecv(t, f.watches, "first watch")

	watchErr := errors.New("connection lost")
	handle.errChannel <- watchErr

	if got := expectRecv(t, f.downs, "OnDown"); !errors.Is(got, watchErr) {
		t.Errorf("OnDown error = %v, expected %v", got, watchErr)
	}
	if closedID := expectRecv(t, f.closes, "close"); closedID != firstID {
		t.Errorf("closed connection %d, expected %d", closedID, firstID)
	}

	secondID := expectRecv(t, f.ups, "second OnUp")
	if secondID == firstID {
		t.Error("reconnect did not produce a new connection")
	}
}

func TestReconnectTriggersTeardown(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	f.fakeConnRes <- connectResult{}
	f.fakeConnRes <- connectResult{}

	f.r.Start()
	defer stopWithDeadline(t, f.r)

	firstID := expectRecv(t, f.ups, "first OnUp")

	reason := errors.New("config changed")
	f.r.Reconnect(reason)

	if got := expectRecv(t, f.downs, "OnDown"); !errors.Is(got, reason) {
		t.Errorf("OnDown error = %v, expected %v", got, reason)
	}
	if closedID := expectRecv(t, f.closes, "close"); closedID != firstID {
		t.Errorf("closed connection %d, expected %d", closedID, firstID)
	}

	expectRecv(t, f.ups, "second OnUp")
}

func TestStaleWatchErrorIgnored(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	f.fakeConnRes <- connectResult{}
	f.fakeConnRes <- connectResult{}

	f.r.Start()
	defer stopWithDeadline(t, f.r)

	expectRecv(t, f.ups, "first OnUp")
	staleHandle := expectRecv(t, f.watches, "first watch")

	staleHandle.errChannel <- errors.New("gen 1 lost")
	_ = expectRecv(t, f.downs, "OnDown")
	expectRecv(t, f.closes, "close of gen 1")

	secondID := expectRecv(t, f.ups, "second OnUp")
	expectRecv(t, f.watches, "second watch")

	// a late error from the first generation's watch must not tear down
	// the second generation's connection
	staleHandle.errChannel <- errors.New("gen 1 late error")

	expectNone(t, f.downs, "OnDown from stale generation")

	if conn, ok := f.r.Current(); !ok || conn.id != secondID {
		t.Errorf("Current() = (%v, %v), expected id %d", conn.id, ok, secondID)
	}
}

func TestEnabledGate(t *testing.T) {
	f := newFakeConnector(fastBackoff, true)

	f.r.Start()
	defer stopWithDeadline(t, f.r)

	// while disabled, the reconnector must not dial at all
	expectNone(t, f.dialStarted, "dial while disabled")

	f.setEnabled(true)
	expectRecv(t, f.dialStarted, "dial after enabling")

	f.fakeConnRes <- connectResult{}
	expectRecv(t, f.ups, "OnUp")
}

func TestDisabledWhileDialing(t *testing.T) {
	f := newFakeConnector(fastBackoff, true)

	f.setEnabled(true)
	f.r.Start()
	defer stopWithDeadline(t, f.r)

	expectRecv(t, f.dialStarted, "dial start")

	f.setEnabled(false)
	f.fakeConnRes <- connectResult{}

	expectRecv(t, f.closes, "close of non-adopted connection")
	expectNone(t, f.ups, "OnUp while disabled")

	if f.r.Connected() {
		t.Error("Connected() = true for a dropped connection")
	}
}

func TestBackoffResetAfterConnection(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	f.fakeConnRes <- connectResult{err: errors.New("fail 1")}
	f.fakeConnRes <- connectResult{err: errors.New("fail 2")}
	f.fakeConnRes <- connectResult{}

	f.r.Start()
	defer stopWithDeadline(t, f.r)

	expectRecv(t, f.ups, "first OnUp")
	handle := expectRecv(t, f.watches, "first watch")

	if a := expectRecv(t, f.backoffs, "backoff 1"); a != 0 {
		t.Errorf("backoff attempt = %d, expected 0", a)
	}
	if a := expectRecv(t, f.backoffs, "backoff 2"); a != 1 {
		t.Errorf("backoff attempt = %d, expected 1", a)
	}

	// losing an established connection must restart the attempt count
	f.fakeConnRes <- connectResult{}
	handle.errChannel <- errors.New("connection lost")

	_ = expectRecv(t, f.downs, "OnDown")
	if a := expectRecv(t, f.backoffs, "backoff after loss"); a != 0 {
		t.Errorf("backoff attempt after loss = %d, expected 0", a)
	}

	expectRecv(t, f.ups, "second OnUp")
}

func TestRestartAfterStop(t *testing.T) {
	f := newFakeConnector(fastBackoff, false)

	f.fakeConnRes <- connectResult{}
	f.r.Start()
	firstID := expectRecv(t, f.ups, "first OnUp")

	stopWithDeadline(t, f.r)
	if closedID := expectRecv(t, f.closes, "close on stop"); closedID != firstID {
		t.Errorf("closed connection %d, expected %d", closedID, firstID)
	}
	if f.r.Connected() {
		t.Error("Connected() = true after Stop")
	}

	f.fakeConnRes <- connectResult{}
	f.r.Start()
	expectRecv(t, f.ups, "OnUp after restart")

	stopWithDeadline(t, f.r)
}
