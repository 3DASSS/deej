package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/nik9play/deej/internal/config"
	"io"
	"strings"
	"sync"
	"time"
)

// discordConn is one Discord IPC connection generation, carrying the config
// snapshot it was opened with so config reloads can detect credential changes
type discordConn struct {
	rw  io.ReadWriteCloser
	cfg config.DiscordSettings

	writeLock sync.Mutex

	// pending correlates replies to the callers waiting on them, by nonce
	pendingLock sync.Mutex
	pending     map[string]chan discordFrame

	// onEvent receives subscribed RPC events. It is called from the read loop,
	// so it must never block
	onEvent func(evt string, data json.RawMessage)

	ready     chan struct{}
	readyOnce sync.Once

	// done is closed when the read loop exits, broadcasting the stored error
	// to every caller waiting on this connection
	done     chan struct{}
	doneOnce sync.Once
	doneErr  error
}

func newDiscordConn(rw io.ReadWriteCloser, cfg config.DiscordSettings, onEvent func(evt string, data json.RawMessage)) *discordConn {
	conn := &discordConn{
		rw:      rw,
		cfg:     cfg,
		onEvent: onEvent,
		pending: map[string]chan discordFrame{},
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
	}

	go conn.readLoop()

	return conn
}

// Close releases the transport, which unblocks the read loop and in turn any
// Watch waiting on it
func (c *discordConn) Close() error {
	return c.rw.Close()
}

// discordFrame is the envelope every RPC command, reply and event shares
type discordFrame struct {
	Cmd   string          `json:"cmd"`
	Nonce string          `json:"nonce,omitempty"`
	Evt   string          `json:"evt,omitempty"`
	Args  any             `json:"args,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func (c *discordConn) request(cmd string, args any) (json.RawMessage, error) {
	return c.roundTrip(discordFrame{Cmd: cmd, Args: args}, discordCommandTimeout)
}

// requestEvent issues SUBSCRIBE and UNSUBSCRIBE, the two commands that name
// their event in evt rather than in args
func (c *discordConn) requestEvent(cmd string, evt string, args any) (json.RawMessage, error) {
	return c.roundTrip(discordFrame{Cmd: cmd, Evt: evt, Args: args}, discordCommandTimeout)
}

func (c *discordConn) requestWithTimeout(cmd string, args any, timeout time.Duration) (json.RawMessage, error) {
	return c.roundTrip(discordFrame{Cmd: cmd, Args: args}, timeout)
}

func (c *discordConn) roundTrip(frame discordFrame, timeout time.Duration) (json.RawMessage, error) {
	cmd := frame.Cmd
	nonce := uuid.NewString()
	frame.Nonce = nonce

	body, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", cmd, err)
	}

	// buffered, so the read loop never blocks handing over a reply nobody is
	// waiting for anymore
	replyChannel := make(chan discordFrame, 1)

	c.pendingLock.Lock()
	c.pending[nonce] = replyChannel
	c.pendingLock.Unlock()

	defer func() {
		c.pendingLock.Lock()
		delete(c.pending, nonce)
		c.pendingLock.Unlock()
	}()

	if err := c.write(discordOpFrame, body); err != nil {
		return nil, err
	}

	select {
	case reply := <-replyChannel:
		return discordReplyData(reply)

	case <-c.done:
		return nil, c.doneErr

	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for a reply to %s", cmd)
	}
}

// discordReplyData unwraps a reply, turning an ERROR frame into a Go error
func discordReplyData(reply discordFrame) (json.RawMessage, error) {
	if reply.Evt != "ERROR" {
		return reply.Data, nil
	}

	var rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(reply.Data, &rpcErr); err == nil && rpcErr.Message != "" {
		return nil, fmt.Errorf("discord rpc error %d: %s", rpcErr.Code, rpcErr.Message)
	}

	return nil, errors.New("discord rpc error")
}

func (c *discordConn) write(opcode uint32, body []byte) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	return writeDiscordFrame(c.rw, opcode, body)
}

func (c *discordConn) fail(err error) {
	c.doneOnce.Do(func() {
		c.doneErr = err
		close(c.done)
	})
}

// readLoop is the connection's only reader: it hands replies to the callers
// waiting on their nonce and answers pings, until the transport fails or is
// closed
func (c *discordConn) readLoop() {
	for {
		opcode, body, err := readDiscordFrame(c.rw)
		if err != nil {
			c.fail(fmt.Errorf("read frame: %w", err))
			return
		}

		switch opcode {
		case discordOpPing:
			// the client wants its own payload echoed back
			if err := c.write(discordOpPong, body); err != nil {
				c.fail(err)
				return
			}
			continue

		case discordOpClose:
			c.fail(fmt.Errorf("discord closed the connection: %s", strings.TrimSpace(string(body))))
			return

		case discordOpFrame:

		default:
			continue
		}

		var frame discordFrame
		if err := json.Unmarshal(body, &frame); err != nil {
			continue
		}

		// the handshake is acknowledged with an event rather than a reply, so
		// it can't be correlated by nonce like everything else
		if frame.Cmd == "DISPATCH" && frame.Evt == "READY" {
			c.readyOnce.Do(func() { close(c.ready) })
			continue
		}

		if frame.Nonce == "" {
			// a subscribed event. onEvent must not block: it's handing work to
			// a goroutine that issues requests this very loop has to answer
			if frame.Cmd == "DISPATCH" && frame.Evt != "" && c.onEvent != nil {
				c.onEvent(frame.Evt, frame.Data)
			}

			continue
		}

		c.pendingLock.Lock()
		replyChannel, waiting := c.pending[frame.Nonce]
		delete(c.pending, frame.Nonce)
		c.pendingLock.Unlock()

		if waiting {
			replyChannel <- frame
		}
	}
}

// discordMaxFrameSize caps what a single frame may claim, so a desynced stream
// can't make deej allocate wildly
const discordMaxFrameSize = 1 << 20

// writeDiscordFrame writes one IPC frame: a little-endian opcode and length,
// then the JSON body
func writeDiscordFrame(w io.Writer, opcode uint32, body []byte) error {
	frame := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], opcode)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(body)))

	if _, err := w.Write(append(frame, body...)); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	return nil
}

func readDiscordFrame(r io.Reader) (uint32, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}

	opcode := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])

	if length > discordMaxFrameSize {
		return 0, nil, fmt.Errorf("frame too large: %d bytes", length)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}

	return opcode, body, nil
}

// dialDiscordIPC opens the Discord client's local RPC transport. Discord takes
// the first free slot of ten, so all of them get probed
func dialDiscordIPC() (io.ReadWriteCloser, error) {
	for _, base := range discordIPCPaths() {
		for i := range 10 {
			rw, err := openDiscordIPC(fmt.Sprintf("%s%d", base, i))
			if err != nil {
				continue
			}

			return rw, nil
		}
	}

	return nil, errors.New("no Discord IPC socket found (is Discord running?)")
}
