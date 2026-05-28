package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSHub manages WebSocket connections and broadcasts events to all clients.
type WSHub struct {
	mu         sync.Mutex
	clients    map[*wsConn]bool
	broadcast  chan []byte
	register   chan *wsConn
	unregister chan *wsConn
}

type wsConn struct {
	hub  *WSHub
	conn *websocket.Conn
	send chan []byte
}

// NewWSRouter creates a minimal http.Handler that only serves the WebSocket endpoint.
func NewWSRouter(hub *WSHub) http.Handler {
	r := chi.NewRouter()
	r.Get("/ws", hub.ServeWS)
	return r
}

func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*wsConn]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *wsConn),
		unregister: make(chan *wsConn),
	}
}

func (h *WSHub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = true
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.Lock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					close(c.send)
					delete(h.clients, c)
				}
			}
			h.mu.Unlock()
		}
	}
}

// Publish sends a typed event to all connected WebSocket clients.
func (h *WSHub) Publish(eventType string, data any) {
	msg, err := json.Marshal(map[string]any{"type": eventType, "data": data})
	if err != nil {
		return
	}
	select {
	case h.broadcast <- msg:
	default:
	}
}

func (h *WSHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsConn{hub: h, conn: conn, send: make(chan []byte, 256)}
	h.register <- c
	go c.writePump()
	go c.readPump()
}

func (c *wsConn) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *wsConn) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}
