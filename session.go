package gowebsocket

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Session wrapper around websocket connections.
type Session struct {
	Request     *http.Request
	Keys        map[string]interface{}
	conn        *websocket.Conn
	output      chan *envelope
	gowebsocket *GoWebSocket
	open        bool
	rwmutex     *sync.RWMutex
}

func (s *Session) writeMessage(message *envelope) {
	if s.closed() {
		s.gowebsocket.errorHandler(s, errors.New("tried to write to closed a session"))
		return
	}

	select {
	case s.output <- message:
	default:
		s.gowebsocket.errorHandler(s, errors.New("session message buffer is full"))
	}
}

func (s *Session) writeRaw(message *envelope) error {
	if s.closed() {
		return errors.New("tried to write to a closed session")
	}

	s.conn.SetWriteDeadline(time.Now().Add(s.gowebsocket.Config.WriteWait))
	err := s.conn.WriteMessage(message.t, message.msg)

	if err != nil {
		return err
	}

	return nil
}

func (s *Session) closed() bool {
	s.rwmutex.RLock()
	defer s.rwmutex.RUnlock()

	return !s.open
}

func (s *Session) close() {
	if !s.closed() {
		s.rwmutex.Lock()
		s.open = false
		s.conn.Close()
		close(s.output)
		s.rwmutex.Unlock()
	}
}

func (s *Session) ping() {
	s.writeRaw(&envelope{t: websocket.PingMessage, msg: []byte{}})
}

func (s *Session) writePump() {
	ticker := time.NewTicker(s.gowebsocket.Config.PingPeriod)
	defer ticker.Stop()

loop:
	for {
		select {
		case msg, ok := <-s.output:
			if !ok {
				break loop
			}

			err := s.writeRaw(msg)

			if err != nil {
				s.gowebsocket.errorHandler(s, err)
				break loop
			}

			if msg.t == websocket.CloseMessage {
				break loop
			}

			if msg.t == websocket.TextMessage {
				s.gowebsocket.messageSentHandler(s, msg.msg)
			}

			if msg.t == websocket.BinaryMessage {
				s.gowebsocket.messageSentHandlerBinary(s, msg.msg)
			}
		case <-ticker.C:
			s.ping()
		}
	}
}

func (s *Session) readPump() {
	s.conn.SetReadLimit(s.gowebsocket.Config.MaxMessageSize)
	s.conn.SetReadDeadline(time.Now().Add(s.gowebsocket.Config.PongWait))

	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(s.gowebsocket.Config.PongWait))
		s.gowebsocket.pongHandler(s)
		return nil
	})

	if s.gowebsocket.closeHandler != nil {
		s.conn.SetCloseHandler(func(code int, text string) error {
			return s.gowebsocket.closeHandler(s, code, text)
		})
	}

	for {
		t, message, err := s.conn.ReadMessage()

		if err != nil {
			s.gowebsocket.errorHandler(s, err)
			break
		}

		if t == websocket.TextMessage {
			s.gowebsocket.messageHandler(s, message)
		}

		if t == websocket.BinaryMessage {
			s.gowebsocket.messageHandlerBinary(s, message)
		}
	}
}

// Write writes message to session.
func (s *Session) Write(msg []byte) error {
	if s.closed() {
		return errors.New("session is closed")
	}

	s.writeMessage(&envelope{t: websocket.TextMessage, msg: msg})

	return nil
}

// WriteBinary writes a binary message to session.
func (s *Session) WriteBinary(msg []byte) error {
	if s.closed() {
		return errors.New("session is closed")
	}

	s.writeMessage(&envelope{t: websocket.BinaryMessage, msg: msg})

	return nil
}

// Close closes session.
func (s *Session) Close() error {
	if s.closed() {
		return errors.New("session is already closed")
	}

	s.writeMessage(&envelope{t: websocket.CloseMessage, msg: []byte{}})

	return nil
}

// CloseWithMsg closes the session with the provided payload.
// Use the FormatCloseMessage function to format a proper close message payload.
func (s *Session) CloseWithMsg(msg []byte) error {
	if s.closed() {
		return errors.New("session is already closed")
	}

	s.writeMessage(&envelope{t: websocket.CloseMessage, msg: msg})

	return nil
}

// Set is used to store a new key/value pair exclusivelly for this session.
// It also lazy initializes s.Keys if it was not used previously.
func (s *Session) Set(key string, value interface{}) {
	if s.Keys == nil {
		s.Keys = make(map[string]interface{})
	}

	s.Keys[key] = value
}

// Get returns the value for the given key, ie: (value, true).
// If the value does not exists it returns (nil, false)
func (s *Session) Get(key string) (value interface{}, exists bool) {
	if s.Keys != nil {
		value, exists = s.Keys[key]
	}

	return
}

// MustGet returns the value for the given key if it exists, otherwise it panics.
func (s *Session) MustGet(key string) interface{} {
	if value, exists := s.Get(key); exists {
		return value
	}

	panic("Key \"" + key + "\" does not exist")
}

// IsClosed returns the status of the connection.
func (s *Session) IsClosed() bool {
	return s.closed()
}
