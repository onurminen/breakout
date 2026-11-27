package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// A window close publishes one result per symbol at once, so a client
// following thousands of symbols needs room for that burst.
const sendQueueSize = 4096

type outbound struct {
	data []byte
	kind string
}

type hub struct {
	mu       sync.Mutex
	bySymbol map[string]map[*wsClient]struct{}
	clients  map[*wsClient]struct{}
}

func newHub() *hub {
	return &hub{bySymbol: make(map[string]map[*wsClient]struct{}), clients: make(map[*wsClient]struct{})}
}

type wsClient struct {
	hub  *hub
	conn *websocket.Conn
	send chan outbound
	stop chan struct{}
	once sync.Once
	subs map[string]struct{} // guarded by hub.mu
}

func (h *hub) add(conn *websocket.Conn) *wsClient {
	c := &wsClient{hub: h, conn: conn, send: make(chan outbound, sendQueueSize), stop: make(chan struct{}), subs: make(map[string]struct{})}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	wsClients.Inc()
	h.mu.Unlock()
	return c
}

// change queues the confirmation before releasing the hub lock, so a live
// message for a newly subscribed symbol cannot overtake its confirmation.
func (h *hub) change(c *wsClient, names []string, subscribe bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-c.stop:
		return false
	default:
	}
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		unique[name] = struct{}{}
		if subscribe {
			if h.bySymbol[name] == nil {
				h.bySymbol[name] = make(map[*wsClient]struct{})
			}
			h.bySymbol[name][c] = struct{}{}
			c.subs[name] = struct{}{}
		} else {
			delete(c.subs, name)
			delete(h.bySymbol[name], c)
			if len(h.bySymbol[name]) == 0 {
				delete(h.bySymbol, name)
			}
		}
	}
	confirmed := make([]string, 0, len(unique))
	for name := range unique {
		confirmed = append(confirmed, name)
	}
	sort.Strings(confirmed)
	kind := "subscribed"
	if !subscribe {
		kind = "unsubscribed"
	}
	data, _ := json.Marshal(struct {
		Type    string   `json:"type"`
		Symbols []string `json:"symbols"`
	}{kind, confirmed})
	select {
	case c.send <- outbound{data: data, kind: kind}:
		return true
	default:
		return false
	}
}

func (h *hub) publish(symbol, kind string, data []byte) {
	h.mu.Lock()
	var slow []*wsClient
	for c := range h.bySymbol[symbol] {
		select {
		case c.send <- outbound{data: data, kind: kind}:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.Unlock()
	for _, c := range slow {
		c.close("slow")
	}
}

func (c *wsClient) close(reason string) {
	c.once.Do(func() {
		h := c.hub
		h.mu.Lock()
		delete(h.clients, c)
		for name := range c.subs {
			delete(h.bySymbol[name], c)
			if len(h.bySymbol[name]) == 0 {
				delete(h.bySymbol, name)
			}
		}
		wsClients.Dec()
		wsDisconnects.WithLabelValues(reason).Inc()
		close(c.stop)
		h.mu.Unlock()
		if c.conn != nil {
			_ = c.conn.CloseNow()
		}
	})
}

func (h *hub) closeAll() {
	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.close("shutdown")
	}
}

func (c *wsClient) write(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		case msg := <-c.send:
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg.data)
			cancel()
			if err != nil {
				c.close("write_error")
				return
			}
			wsMessages.WithLabelValues(msg.kind).Inc()
		}
	}
}

func validSymbols(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		if name == "" || strings.TrimSpace(name) != name {
			return false
		}
	}
	return true
}
