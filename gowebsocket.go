package gowebsocket

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Close codes defined in RFC 6455, section 11.7.
// Duplicate of codes from gorilla/websocket for convenience.
const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005
	CloseAbnormalClosure         = 1006
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
	CloseServiceRestart          = 1012
	CloseTryAgainLater           = 1013
	CloseTLSHandshake            = 1015
)

// Duplicate of codes from gorilla/websocket for convenience.
var validReceivedCloseCodes = map[int]bool{
	// see http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number

	CloseNormalClosure:           true,
	CloseGoingAway:               true,
	CloseProtocolError:           true,
	CloseUnsupportedData:         true,
	CloseNoStatusReceived:        false,
	CloseAbnormalClosure:         false,
	CloseInvalidFramePayloadData: true,
	ClosePolicyViolation:         true,
	CloseMessageTooBig:           true,
	CloseMandatoryExtension:      true,
	CloseInternalServerErr:       true,
	CloseServiceRestart:          true,
	CloseTryAgainLater:           true,
	CloseTLSHandshake:            false,
}

type handleMessageFunc func(*Session, []byte)
type handleErrorFunc func(*Session, error)
type handleCloseFunc func(*Session, int, string) error
type handleSessionFunc func(*Session)
type filterFunc func(*Session) bool

// GoWebSocket implements a websocket manager.
type GoWebSocket struct {
	Config                   *Config
	Upgrader                 *websocket.Upgrader
	messageHandler           handleMessageFunc
	messageHandlerBinary     handleMessageFunc
	messageSentHandler       handleMessageFunc
	messageSentHandlerBinary handleMessageFunc
	errorHandler             handleErrorFunc
	closeHandler             handleCloseFunc
	connectHandler           handleSessionFunc
	disconnectHandler        handleSessionFunc
	pongHandler              handleSessionFunc
	hub                      *hub
}

// New creates a new gowebsocket instance with default Upgrader and Config.
func New() *GoWebSocket {
	upgrader := &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	hub := newHub()

	go hub.run()

	return &GoWebSocket{
		Config:                   newConfig(),
		Upgrader:                 upgrader,
		messageHandler:           func(*Session, []byte) {},
		messageHandlerBinary:     func(*Session, []byte) {},
		messageSentHandler:       func(*Session, []byte) {},
		messageSentHandlerBinary: func(*Session, []byte) {},
		errorHandler:             func(*Session, error) {},
		closeHandler:             nil,
		connectHandler:           func(*Session) {},
		disconnectHandler:        func(*Session) {},
		pongHandler:              func(*Session) {},
		hub:                      hub,
	}
}

// HandleConnect fires fn when a session connects.
func (m *GoWebSocket) HandleConnect(fn func(*Session)) {
	m.connectHandler = fn
}

// HandleDisconnect fires fn when a session disconnects.
func (m *GoWebSocket) HandleDisconnect(fn func(*Session)) {
	m.disconnectHandler = fn
}

// HandlePong fires fn when a pong is received from a session.
func (m *GoWebSocket) HandlePong(fn func(*Session)) {
	m.pongHandler = fn
}

// HandleMessage fires fn when a text message comes in.
func (m *GoWebSocket) HandleMessage(fn func(*Session, []byte)) {
	m.messageHandler = fn
}

// HandleMessageBinary fires fn when a binary message comes in.
func (m *GoWebSocket) HandleMessageBinary(fn func(*Session, []byte)) {
	m.messageHandlerBinary = fn
}

// HandleSentMessage fires fn when a text message is successfully sent.
func (m *GoWebSocket) HandleSentMessage(fn func(*Session, []byte)) {
	m.messageSentHandler = fn
}

// HandleSentMessageBinary fires fn when a binary message is successfully sent.
func (m *GoWebSocket) HandleSentMessageBinary(fn func(*Session, []byte)) {
	m.messageSentHandlerBinary = fn
}

// HandleError fires fn when a session has an error.
func (m *GoWebSocket) HandleError(fn func(*Session, error)) {
	m.errorHandler = fn
}

// HandleClose sets the handler for close messages received from the session.
// The code argument to h is the received close code or CloseNoStatusReceived
// if the close message is empty. The default close handler sends a close frame
// back to the session.
//
// The application must read the connection to process close messages as
// described in the section on Control Frames above.
//
// The connection read methods return a CloseError when a close frame is
// received. Most applications should handle close messages as part of their
// normal error handling. Applications should only set a close handler when the
// application must perform some action before sending a close frame back to
// the session.
func (m *GoWebSocket) HandleClose(fn func(*Session, int, string) error) {
	if fn != nil {
		m.closeHandler = fn
	}
}

// HandleRequest upgrades http requests to websocket connections and dispatches them to be handled by the gowebsocket instance.
func (m *GoWebSocket) HandleRequest(w http.ResponseWriter, r *http.Request) error {
	return m.HandleRequestWithKeys(w, r, nil)
}

// HandleRequestWithKeys does the same as HandleRequest but populates session.Keys with keys.
func (m *GoWebSocket) HandleRequestWithKeys(w http.ResponseWriter, r *http.Request, keys map[string]interface{}) error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is closed")
	}

	conn, err := m.Upgrader.Upgrade(w, r, w.Header())

	if err != nil {
		return err
	}

	session := &Session{
		Request:     r,
		Keys:        keys,
		conn:        conn,
		output:      make(chan *envelope, m.Config.MessageBufferSize),
		gowebsocket: m,
		open:        true,
		rwmutex:     &sync.RWMutex{},
	}

	m.hub.register <- session

	m.connectHandler(session)

	go session.writePump()

	session.readPump()

	if !m.hub.closed() {
		m.hub.unregister <- session
	}

	session.close()

	m.disconnectHandler(session)

	return nil
}

// Broadcast broadcasts a text message to all sessions.
func (m *GoWebSocket) Broadcast(msg []byte) error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is closed")
	}

	message := &envelope{t: websocket.TextMessage, msg: msg}
	m.hub.broadcast <- message

	return nil
}

// BroadcastFilter broadcasts a text message to all sessions that fn returns true for.
func (m *GoWebSocket) BroadcastFilter(msg []byte, fn func(*Session) bool) error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is closed")
	}

	message := &envelope{t: websocket.TextMessage, msg: msg, filter: fn}
	m.hub.broadcast <- message

	return nil
}

// BroadcastOthers broadcasts a text message to all sessions except session s.
func (m *GoWebSocket) BroadcastOthers(msg []byte, s *Session) error {
	return m.BroadcastFilter(msg, func(q *Session) bool {
		return s != q
	})
}

// BroadcastMultiple broadcasts a text message to multiple sessions given in the sessions slice.
func (m *GoWebSocket) BroadcastMultiple(msg []byte, sessions []*Session) error {
	for _, sess := range sessions {
		if writeErr := sess.Write(msg); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// BroadcastBinary broadcasts a binary message to all sessions.
func (m *GoWebSocket) BroadcastBinary(msg []byte) error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is closed")
	}

	message := &envelope{t: websocket.BinaryMessage, msg: msg}
	m.hub.broadcast <- message

	return nil
}

// BroadcastBinaryFilter broadcasts a binary message to all sessions that fn returns true for.
func (m *GoWebSocket) BroadcastBinaryFilter(msg []byte, fn func(*Session) bool) error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is closed")
	}

	message := &envelope{t: websocket.BinaryMessage, msg: msg, filter: fn}
	m.hub.broadcast <- message

	return nil
}

// BroadcastBinaryOthers broadcasts a binary message to all sessions except session s.
func (m *GoWebSocket) BroadcastBinaryOthers(msg []byte, s *Session) error {
	return m.BroadcastBinaryFilter(msg, func(q *Session) bool {
		return s != q
	})
}

// Close closes the gowebsocket instance and all connected sessions.
func (m *GoWebSocket) Close() error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is already closed")
	}

	m.hub.exit <- &envelope{t: websocket.CloseMessage, msg: []byte{}}

	return nil
}

// CloseWithMsg closes the gowebsocket instance with the given close payload and all connected sessions.
// Use the FormatCloseMessage function to format a proper close message payload.
func (m *GoWebSocket) CloseWithMsg(msg []byte) error {
	if m.hub.closed() {
		return errors.New("gowebsocket instance is already closed")
	}

	m.hub.exit <- &envelope{t: websocket.CloseMessage, msg: msg}

	return nil
}

// Len return the number of connected sessions.
func (m *GoWebSocket) Len() int {
	return m.hub.len()
}

// IsClosed returns the status of the gowebsocket instance.
func (m *GoWebSocket) IsClosed() bool {
	return m.hub.closed()
}

// FormatCloseMessage formats closeCode and text as a WebSocket close message.
func FormatCloseMessage(closeCode int, text string) []byte {
	return websocket.FormatCloseMessage(closeCode, text)
}
