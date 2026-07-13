package deej

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jfreymuth/pulse/proto"
	"go.uber.org/zap"

	"github.com/nik9play/deej/pkg/reconnect"
)

const (
	// buffer size for the event work channel
	paWorkChanSize = 512

	// how often watch pings the server to catch silently dead connections
	paKeepaliveInterval = 30 * time.Second
)

// paConn is one PulseAudio connection generation.
type paConn struct {
	client    *proto.Client
	conn      net.Conn
	closedCh  chan struct{}
	closeOnce *sync.Once
	logger    *zap.SugaredLogger
}

func (c paConn) forceClose() {
	c.closeOnce.Do(func() { close(c.closedCh) })
}

// request performs one round-trip on this connection
func (c paConn) request(req proto.RequestArgs, rpl proto.Reply) error {
	err := c.client.Request(req, rpl)
	if err == nil {
		return nil
	}

	var protoErr proto.Error
	if !errors.As(err, &protoErr) {
		c.logger.Debugw("Transport-level request failure, marking connection closed", "error", err)
		c.forceClose()
	}

	return err
}

type paSessionFinder struct {
	logger        *zap.SugaredLogger
	sessionLogger *zap.SugaredLogger

	reconnector *reconnect.Reconnector[paConn]

	// mu guards the session maps
	mu           sync.Mutex
	masterSink   *masterSession
	masterSource *masterSession
	sinkInputs   map[uint32]*paSession

	// named device sessions (by index)
	namedSinks   map[uint32]*masterSession
	namedSources map[uint32]*masterSession

	// receives session events synchronously
	handler SessionEventHandler
	started bool

	// workChan receives jobs to be executed serially on the worker goroutine,
	// preserving the order in which PulseAudio delivered the events
	workChan chan func()

	stopCh chan struct{}
}

func newSessionFinder(logger *zap.SugaredLogger) (SessionFinder, error) {
	sf := &paSessionFinder{
		logger:        logger.Named("session_finder"),
		sessionLogger: logger.Named("sessions"),
		sinkInputs:    make(map[uint32]*paSession),
		namedSinks:    make(map[uint32]*masterSession),
		namedSources:  make(map[uint32]*masterSession),
		workChan:      make(chan func(), paWorkChanSize),
		stopCh:        make(chan struct{}),
	}

	sf.reconnector = reconnect.New(reconnect.Options[paConn]{
		Logger: sf.logger,
		Dial:   sf.dial,
		Watch:  sf.watch,
		Close:  sf.close,
		OnUp:   sf.onUp,
		OnDown: sf.onDown,
	})

	sf.logger.Debug("Created event-driven PA session finder")
	return sf, nil
}

// Begins session discovery, delivering events synchronously to handler.
func (sf *paSessionFinder) Start(handler SessionEventHandler) error {
	if sf.started {
		return errors.New("session finder already started")
	}
	sf.started = true
	sf.handler = handler

	// the worker must be running before the first connection is made, since
	// onUp queues the initial enumeration on it
	go sf.eventWorker()
	sf.reconnector.Start()

	return nil
}

// eventWorker executes queued jobs one at a time, so session add/remove
// events are always processed in the order PulseAudio delivered them
func (sf *paSessionFinder) eventWorker() {
	for {
		select {
		case <-sf.stopCh:
			return
		case work := <-sf.workChan:
			work()
		}
	}
}

// dispatchWork queues fn for execution on the worker goroutine. It must not
// block: it is called from the pulse client's read loop, which also delivers
// the replies our handlers wait for
func (sf *paSessionFinder) dispatchWork(fn func()) {
	select {
	case sf.workChan <- fn:
	default:
		sf.logger.Warn("Event work channel full, dropping event")
	}
}

// dial connects to PulseAudio and completes the handshake (client name,
// event subscription) without touching any shared state - the reconnector
// decides whether to adopt the returned connection
func (sf *paSessionFinder) dial() (paConn, error) {
	client, conn, err := proto.Connect("")
	if err != nil {
		return paConn{}, fmt.Errorf("connect to PulseAudio: %w", err)
	}

	newConn := paConn{
		client:    client,
		conn:      conn,
		closedCh:  make(chan struct{}),
		closeOnce: &sync.Once{},
		logger:    sf.logger,
	}

	// the callback belongs to this connection only: subscription events are
	// dispatched to the shared work queue, but a ConnectionClosed can only
	// signal its own generation's closed channel, so a stale callback can't
	// tear down a newer connection
	client.Callback = func(msg any) {
		switch v := msg.(type) {
		case *proto.SubscribeEvent:
			sf.handleSubscribeEvent(v)
		case *proto.ConnectionClosed:
			newConn.forceClose()
		}
	}

	if err := client.Request(&proto.SetClientName{
		Props: proto.PropList{"application.name": proto.PropListString("deej")},
	}, &proto.SetClientNameReply{}); err != nil {
		_ = conn.Close()
		return paConn{}, fmt.Errorf("set client name: %w", err)
	}

	if err := client.Request(&proto.Subscribe{
		Mask: proto.SubscriptionMaskSinkInput | proto.SubscriptionMaskServer | proto.SubscriptionMaskSink | proto.SubscriptionMaskSource,
	}, nil); err != nil {
		_ = conn.Close()
		return paConn{}, fmt.Errorf("subscribe to events: %w", err)
	}

	return newConn, nil
}

// watch blocks until this connection dies. detection has two prongs: the
// connection's callback observing ConnectionClosed (which the pulse client
// only fires on clean EOF), and transport-level request failures - including
// the periodic keepalive here - marking the connection closed via forceClose
func (sf *paSessionFinder) watch(conn paConn, errChannel chan<- error) {
	keepalive := time.NewTicker(paKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-conn.closedCh:
			select {
			case errChannel <- errors.New("PulseAudio connection closed"):
			default:
			}
			return

		case <-keepalive.C:
			// a failed keepalive marks the connection closed inside
			// conn.request; the next loop iteration picks that up
			_ = conn.request(&proto.GetServerInfo{}, &proto.GetServerInfoReply{})
		}
	}
}

func (sf *paSessionFinder) close(conn paConn) {
	_ = conn.conn.Close()

	conn.forceClose()

	sf.logger.Debug("Closed PulseAudio connection")
}

func (sf *paSessionFinder) onUp(_ paConn) {
	sf.logger.Info("Connected to PulseAudio")

	// queue the initial state sync
	sf.dispatchWork(func() {
		sf.refreshMaster()
		sf.enumerateExistingSessions()
		sf.enumerateExistingDevices()
	})
}

func (sf *paSessionFinder) onDown(err error) {
	sf.logger.Warnw("PulseAudio connection lost, clearing sessions and reconnecting", "error", err)
	sf.clearSessions()
}

// handleSubscribeEvent routes one subscription event from a connection's
// callback onto the serial work queue
func (sf *paSessionFinder) handleSubscribeEvent(v *proto.SubscribeEvent) {
	eventType := v.Event.GetType()
	index := v.Index

	switch v.Event.GetFacility() {
	case proto.EventSinkSinkInput:
		sf.dispatchWork(func() { sf.handleSinkInputEvent(eventType, index) })
	case proto.EventServer:
		sf.dispatchWork(sf.refreshMaster)
	case proto.EventSink:
		sf.dispatchWork(func() { sf.handleSinkEvent(eventType, index) })
	case proto.EventSource:
		sf.dispatchWork(func() { sf.handleSourceEvent(eventType, index) })
	}
}

func (sf *paSessionFinder) clearSessions() {
	// collect all sessions under the lock, then notify outside of it
	sf.mu.Lock()

	removed := make([]Session, 0, len(sf.sinkInputs)+len(sf.namedSinks)+len(sf.namedSources)+2)

	for _, s := range sf.sinkInputs {
		removed = append(removed, s)
	}
	sf.sinkInputs = make(map[uint32]*paSession)

	for _, s := range sf.namedSinks {
		removed = append(removed, s)
	}
	sf.namedSinks = make(map[uint32]*masterSession)

	for _, s := range sf.namedSources {
		removed = append(removed, s)
	}
	sf.namedSources = make(map[uint32]*masterSession)

	if sf.masterSink != nil {
		removed = append(removed, sf.masterSink)
		sf.masterSink = nil
	}
	if sf.masterSource != nil {
		removed = append(removed, sf.masterSource)
		sf.masterSource = nil
	}

	sf.mu.Unlock()

	for _, s := range removed {
		sf.notify(SessionEvent{Type: SessionEventRemoved, Session: s})
		s.Release()
	}
}

func (sf *paSessionFinder) handleSinkInputEvent(eventType proto.SubscriptionEventType, index uint32) {
	switch eventType {
	case proto.EventNew:
		sf.addSinkInput(index)
	case proto.EventRemove:
		sf.removeSinkInput(index)
	}
}

func (sf *paSessionFinder) refreshMaster() {
	sf.refreshMasterSink()
	sf.refreshMasterSource()
}

func (sf *paSessionFinder) refreshMasterSink() {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSinkInfoReply{}
	if err := conn.request(&proto.GetSinkInfo{SinkIndex: proto.Undefined}, &reply); err != nil {
		sf.logger.Debugw("Failed to get master sink info", "error", err)
		return
	}

	sf.mu.Lock()
	old := sf.masterSink
	newMaster := newMasterSession(sf.sessionLogger, conn, reply.SinkIndex, reply.Channels, true)
	sf.masterSink = newMaster
	sf.mu.Unlock()

	if old != nil {
		sf.notify(SessionEvent{Type: SessionEventRemoved, Session: old})
		old.Release()
	}
	sf.notify(SessionEvent{Type: SessionEventAdded, Session: newMaster})
}

func (sf *paSessionFinder) refreshMasterSource() {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSourceInfoReply{}
	if err := conn.request(&proto.GetSourceInfo{SourceIndex: proto.Undefined}, &reply); err != nil {
		sf.logger.Debugw("Failed to get master source info", "error", err)
		return
	}

	sf.mu.Lock()
	old := sf.masterSource
	newMaster := newMasterSession(sf.sessionLogger, conn, reply.SourceIndex, reply.Channels, false)
	sf.masterSource = newMaster
	sf.mu.Unlock()

	if old != nil {
		sf.notify(SessionEvent{Type: SessionEventRemoved, Session: old})
		old.Release()
	}
	sf.notify(SessionEvent{Type: SessionEventAdded, Session: newMaster})
}

func (sf *paSessionFinder) enumerateExistingSessions() {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSinkInputInfoListReply{}
	if err := conn.request(&proto.GetSinkInputInfoList{}, &reply); err != nil {
		sf.logger.Errorw("Failed to enumerate sessions", "error", err)
		return
	}

	for _, info := range reply {
		sf.addSinkInputFromInfo(info)
	}
	sf.logger.Debugw("Enumerated sessions", "count", len(reply))
}

func (sf *paSessionFinder) addSinkInput(index uint32) {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSinkInputInfoReply{}
	if err := conn.request(&proto.GetSinkInputInfo{SinkInputIndex: index}, &reply); err != nil {
		sf.logger.Debugw("Failed to get sink input info", "index", index, "error", err)
		return
	}
	sf.addSinkInputFromInfo(&reply)
}

func (sf *paSessionFinder) addSinkInputFromInfo(info *proto.GetSinkInputInfoReply) {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	// Try application.process.binary, then application.id, then application.name
	name, ok := info.Properties["application.process.binary"]
	if !ok {
		name, ok = info.Properties["application.id"]
		if !ok {
			name, ok = info.Properties["application.name"]
			if !ok {
				return
			}
		}
	}

	// friendly name for the settings GUI, e.g. "Firefox"
	var displayName string
	if friendly, ok := info.Properties["application.name"]; ok {
		displayName = friendly.String()
	}

	sf.mu.Lock()
	if _, exists := sf.sinkInputs[info.SinkInputIndex]; exists {
		sf.mu.Unlock()
		return
	}
	session := newPASession(sf.sessionLogger, conn, info.SinkInputIndex, info.Channels, name.String(), displayName)
	sf.sinkInputs[info.SinkInputIndex] = session
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventAdded, Session: session})
	sf.logger.Debugw("Added session", "index", info.SinkInputIndex, "name", name.String())
}

func (sf *paSessionFinder) removeSinkInput(index uint32) {
	sf.mu.Lock()
	session, exists := sf.sinkInputs[index]
	if !exists {
		sf.mu.Unlock()
		return
	}
	delete(sf.sinkInputs, index)
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventRemoved, Session: session})
	session.Release()
	sf.logger.Debugw("Removed session", "index", index)
}

func (sf *paSessionFinder) enumerateExistingDevices() {
	sf.enumerateExistingSinks()
	sf.enumerateExistingSources()
}

func (sf *paSessionFinder) enumerateExistingSinks() {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSinkInfoListReply{}
	if err := conn.request(&proto.GetSinkInfoList{}, &reply); err != nil {
		sf.logger.Errorw("Failed to enumerate sinks", "error", err)
		return
	}

	for _, info := range reply {
		sf.addSinkFromInfo(info)
	}
	sf.logger.Debugw("Enumerated sinks", "count", len(reply))
}

func (sf *paSessionFinder) enumerateExistingSources() {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSourceInfoListReply{}
	if err := conn.request(&proto.GetSourceInfoList{}, &reply); err != nil {
		sf.logger.Errorw("Failed to enumerate sources", "error", err)
		return
	}

	for _, info := range reply {
		sf.addSourceFromInfo(info)
	}
	sf.logger.Debugw("Enumerated sources", "count", len(reply))
}

func (sf *paSessionFinder) handleSinkEvent(eventType proto.SubscriptionEventType, index uint32) {
	switch eventType {
	case proto.EventNew:
		sf.addSink(index)
	case proto.EventRemove:
		sf.removeSink(index)
	}
}

func (sf *paSessionFinder) handleSourceEvent(eventType proto.SubscriptionEventType, index uint32) {
	switch eventType {
	case proto.EventNew:
		sf.addSource(index)
	case proto.EventRemove:
		sf.removeSource(index)
	}
}

func (sf *paSessionFinder) addSink(index uint32) {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSinkInfoReply{}
	if err := conn.request(&proto.GetSinkInfo{SinkIndex: index}, &reply); err != nil {
		sf.logger.Debugw("Failed to get sink info", "index", index, "error", err)
		return
	}
	sf.addSinkFromInfo(&reply)
}

func (sf *paSessionFinder) addSinkFromInfo(info *proto.GetSinkInfoReply) {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	// Get description from properties, fallback to sink name
	description := info.Device
	if description == "" {
		if desc, ok := info.Properties["device.description"]; ok {
			description = desc.String()
		}
	}

	sf.mu.Lock()
	if _, exists := sf.namedSinks[info.SinkIndex]; exists {
		sf.mu.Unlock()
		return
	}
	session := newNamedMasterSession(sf.sessionLogger, conn, info.SinkIndex, info.Channels, true, description)
	session.device = true
	sf.namedSinks[info.SinkIndex] = session
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventAdded, Session: session})
	sf.logger.Debugw("Added named sink", "index", info.SinkIndex, "description", description)
}

func (sf *paSessionFinder) addSource(index uint32) {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	reply := proto.GetSourceInfoReply{}
	if err := conn.request(&proto.GetSourceInfo{SourceIndex: index}, &reply); err != nil {
		sf.logger.Debugw("Failed to get source info", "index", index, "error", err)
		return
	}
	sf.addSourceFromInfo(&reply)
}

func (sf *paSessionFinder) addSourceFromInfo(info *proto.GetSourceInfoReply) {
	conn, ok := sf.reconnector.Current()
	if !ok {
		return
	}

	// Skip monitor sources (they mirror sink outputs)
	if info.MonitorSourceName != "" {
		return
	}

	// Get description from properties, fallback to source name
	description := info.Device
	if description == "" {
		if desc, ok := info.Properties["device.description"]; ok {
			description = desc.String()
		}
	}

	sf.mu.Lock()
	if _, exists := sf.namedSources[info.SourceIndex]; exists {
		sf.mu.Unlock()
		return
	}
	session := newNamedMasterSession(sf.sessionLogger, conn, info.SourceIndex, info.Channels, false, description)
	session.device = true
	sf.namedSources[info.SourceIndex] = session
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventAdded, Session: session})
	sf.logger.Debugw("Added named source", "index", info.SourceIndex, "description", description)
}

func (sf *paSessionFinder) removeSink(index uint32) {
	sf.mu.Lock()
	session, exists := sf.namedSinks[index]
	if !exists {
		sf.mu.Unlock()
		return
	}
	delete(sf.namedSinks, index)
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventRemoved, Session: session})
	session.Release()
	sf.logger.Debugw("Removed named sink", "index", index)
}

func (sf *paSessionFinder) removeSource(index uint32) {
	sf.mu.Lock()
	session, exists := sf.namedSources[index]
	if !exists {
		sf.mu.Unlock()
		return
	}
	delete(sf.namedSources, index)
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventRemoved, Session: session})
	session.Release()
	sf.logger.Debugw("Removed named source", "index", index)
}

// notify delivers a session event to the handler synchronously. For removed
// events the session must stay valid until this returns; the caller may
// release it afterwards
func (sf *paSessionFinder) notify(event SessionEvent) {
	sf.handler(event)
}

func (sf *paSessionFinder) Release() error {
	sf.reconnector.Stop()
	close(sf.stopCh)

	sf.logger.Debug("Released PA session finder")
	return nil
}
