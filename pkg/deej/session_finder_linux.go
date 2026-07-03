package deej

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jfreymuth/pulse/proto"
	"go.uber.org/zap"
)

const (
	reconnectDelay = 2 * time.Second

	// buffer size for the event work channel
	paWorkChanSize = 512
)

type paSessionFinder struct {
	logger        *zap.SugaredLogger
	sessionLogger *zap.SugaredLogger

	mu           sync.RWMutex
	client       *proto.Client
	conn         net.Conn
	masterSink   *masterSession
	masterSource *masterSession
	sinkInputs   map[uint32]*paSession

	// named device sessions (by index)
	namedSinks   map[uint32]*masterSession
	namedSources map[uint32]*masterSession

	// receives session events synchronously; set once by Start before any
	// connection is made
	handler SessionEventHandler
	started bool

	// workChan receives jobs to be executed serially on the worker goroutine,
	// preserving the order in which PulseAudio delivered the events
	workChan chan func()

	reconnectCh chan struct{}
	stopCh      chan struct{}
}

func newSessionFinder(logger *zap.SugaredLogger) (SessionFinder, error) {
	sf := &paSessionFinder{
		logger:        logger.Named("session_finder"),
		sessionLogger: logger.Named("sessions"),
		sinkInputs:    make(map[uint32]*paSession),
		namedSinks:    make(map[uint32]*masterSession),
		namedSources:  make(map[uint32]*masterSession),
		workChan:      make(chan func(), paWorkChanSize),
		reconnectCh:   make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
	}

	sf.logger.Debug("Created event-driven PA session finder")
	return sf, nil
}

// Start begins session discovery, delivering events synchronously to handler
func (sf *paSessionFinder) Start(handler SessionEventHandler) error {
	if sf.started {
		return errors.New("session finder already started")
	}
	sf.started = true
	sf.handler = handler

	if err := sf.connect(); err != nil {
		return err
	}

	go sf.eventWorker()
	go sf.connectionManager()

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

func (sf *paSessionFinder) connectionManager() {
	for {
		select {
		case <-sf.stopCh:
			return
		case <-sf.reconnectCh:
			sf.handleReconnect()
		}
	}
}

func (sf *paSessionFinder) handleReconnect() {
	sf.clearSessions()

	for {
		select {
		case <-sf.stopCh:
			return
		default:
		}

		sf.logger.Debug("Attempting to reconnect to PulseAudio")
		if err := sf.connect(); err != nil {
			sf.logger.Debugw("Reconnect failed, retrying", "error", err)
			time.Sleep(reconnectDelay)
			continue
		}
		sf.logger.Info("Reconnected to PulseAudio")
		return
	}
}

func (sf *paSessionFinder) clearSessions() {
	// collect all sessions under the lock, then notify outside of it: the
	// handler runs synchronously and must not be called while holding sf.mu
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

	if sf.conn != nil {
		sf.conn.Close()
		sf.conn = nil
	}
	sf.client = nil

	sf.mu.Unlock()

	for _, s := range removed {
		sf.notify(SessionEvent{Type: SessionEventRemoved, Session: s})
		s.Release()
	}
}

func (sf *paSessionFinder) connect() error {
	client, conn, err := proto.Connect("")
	if err != nil {
		return fmt.Errorf("connect to PulseAudio: %w", err)
	}

	client.Callback = sf.onPulseEvent

	if err := client.Request(&proto.SetClientName{
		Props: proto.PropList{"application.name": proto.PropListString("deej")},
	}, &proto.SetClientNameReply{}); err != nil {
		conn.Close()
		return fmt.Errorf("set client name: %w", err)
	}

	sf.mu.Lock()
	sf.client = client
	sf.conn = conn
	sf.mu.Unlock()

	// queue the initial enumeration before subscribing: subscription events go
	// through the same queue, so anything that changes during or after the
	// enumeration is processed strictly after it. subscribing after
	// enumerating directly (the old order) would lose sessions created in
	// between, with no periodic refresh to ever pick them up
	sf.dispatchWork(func() {
		sf.refreshMaster()
		sf.enumerateExistingSessions()
		sf.enumerateExistingDevices()
	})

	if err := client.Request(&proto.Subscribe{
		Mask: proto.SubscriptionMaskSinkInput | proto.SubscriptionMaskServer | proto.SubscriptionMaskSink | proto.SubscriptionMaskSource,
	}, nil); err != nil {
		conn.Close()
		return fmt.Errorf("subscribe to events: %w", err)
	}

	return nil
}

func (sf *paSessionFinder) requestReconnect() {
	select {
	case sf.reconnectCh <- struct{}{}:
	default:
	}
}

func (sf *paSessionFinder) onPulseEvent(msg any) {
	switch v := msg.(type) {
	case *proto.SubscribeEvent:
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
	case *proto.ConnectionClosed:
		sf.logger.Warn("PulseAudio connection closed, trying to reconnect")
		sf.requestReconnect()
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
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSinkInfoReply{}
	if err := client.Request(&proto.GetSinkInfo{SinkIndex: proto.Undefined}, &reply); err != nil {
		sf.logger.Debugw("Failed to get master sink info", "error", err)
		return
	}

	sf.mu.Lock()
	old := sf.masterSink
	newMaster := newMasterSession(sf.sessionLogger, sf.client, reply.SinkIndex, reply.Channels, true)
	sf.masterSink = newMaster
	sf.mu.Unlock()

	if old != nil {
		sf.notify(SessionEvent{Type: SessionEventRemoved, Session: old})
		old.Release()
	}
	sf.notify(SessionEvent{Type: SessionEventAdded, Session: newMaster})
}

func (sf *paSessionFinder) refreshMasterSource() {
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSourceInfoReply{}
	if err := client.Request(&proto.GetSourceInfo{SourceIndex: proto.Undefined}, &reply); err != nil {
		sf.logger.Debugw("Failed to get master source info", "error", err)
		return
	}

	sf.mu.Lock()
	old := sf.masterSource
	newMaster := newMasterSession(sf.sessionLogger, sf.client, reply.SourceIndex, reply.Channels, false)
	sf.masterSource = newMaster
	sf.mu.Unlock()

	if old != nil {
		sf.notify(SessionEvent{Type: SessionEventRemoved, Session: old})
		old.Release()
	}
	sf.notify(SessionEvent{Type: SessionEventAdded, Session: newMaster})
}

func (sf *paSessionFinder) enumerateExistingSessions() {
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSinkInputInfoListReply{}
	if err := client.Request(&proto.GetSinkInputInfoList{}, &reply); err != nil {
		sf.logger.Errorw("Failed to enumerate sessions", "error", err)
		return
	}

	for _, info := range reply {
		sf.addSinkInputFromInfo(info)
	}
	sf.logger.Debugw("Enumerated sessions", "count", len(reply))
}

func (sf *paSessionFinder) addSinkInput(index uint32) {
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSinkInputInfoReply{}
	if err := client.Request(&proto.GetSinkInputInfo{SinkInputIndex: index}, &reply); err != nil {
		sf.logger.Debugw("Failed to get sink input info", "index", index, "error", err)
		return
	}
	sf.addSinkInputFromInfo(&reply)
}

func (sf *paSessionFinder) addSinkInputFromInfo(info *proto.GetSinkInputInfoReply) {
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

	sf.mu.Lock()
	if _, exists := sf.sinkInputs[info.SinkInputIndex]; exists {
		sf.mu.Unlock()
		return
	}
	session := newPASession(sf.sessionLogger, sf.client, info.SinkInputIndex, info.Channels, name.String())
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
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSinkInfoListReply{}
	if err := client.Request(&proto.GetSinkInfoList{}, &reply); err != nil {
		sf.logger.Errorw("Failed to enumerate sinks", "error", err)
		return
	}

	for _, info := range reply {
		sf.addSinkFromInfo(info)
	}
	sf.logger.Debugw("Enumerated sinks", "count", len(reply))
}

func (sf *paSessionFinder) enumerateExistingSources() {
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSourceInfoListReply{}
	if err := client.Request(&proto.GetSourceInfoList{}, &reply); err != nil {
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
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSinkInfoReply{}
	if err := client.Request(&proto.GetSinkInfo{SinkIndex: index}, &reply); err != nil {
		sf.logger.Debugw("Failed to get sink info", "index", index, "error", err)
		return
	}
	sf.addSinkFromInfo(&reply)
}

func (sf *paSessionFinder) addSinkFromInfo(info *proto.GetSinkInfoReply) {
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
	session := newNamedMasterSession(sf.sessionLogger, sf.client, info.SinkIndex, info.Channels, true, description)
	session.device = true
	sf.namedSinks[info.SinkIndex] = session
	sf.mu.Unlock()

	sf.notify(SessionEvent{Type: SessionEventAdded, Session: session})
	sf.logger.Debugw("Added named sink", "index", info.SinkIndex, "description", description)
}

func (sf *paSessionFinder) addSource(index uint32) {
	sf.mu.RLock()
	client := sf.client
	sf.mu.RUnlock()
	if client == nil {
		return
	}

	reply := proto.GetSourceInfoReply{}
	if err := client.Request(&proto.GetSourceInfo{SourceIndex: index}, &reply); err != nil {
		sf.logger.Debugw("Failed to get source info", "index", index, "error", err)
		return
	}
	sf.addSourceFromInfo(&reply)
}

func (sf *paSessionFinder) addSourceFromInfo(info *proto.GetSourceInfoReply) {
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
	session := newNamedMasterSession(sf.sessionLogger, sf.client, info.SourceIndex, info.Channels, false, description)
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
	close(sf.stopCh)

	sf.mu.Lock()
	conn := sf.conn
	sf.mu.Unlock()

	if conn != nil {
		conn.Close()
	}
	sf.logger.Debug("Released PA session finder")
	return nil
}
