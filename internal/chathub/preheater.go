package chathub

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type preheatedConn struct {
	conn    *websocket.Conn
	created time.Time
}

type Preheater struct {
	mu      sync.Mutex
	ready   map[string]*preheatedConn
	dialer  *websocket.Dialer
	header  http.Header
	enabled bool
}

func NewPreheater(dialer *websocket.Dialer, header http.Header) *Preheater {
	p := &Preheater{
		ready:   make(map[string]*preheatedConn),
		dialer:  dialer,
		header:  header,
		enabled: true,
	}
	go p.gc()
	return p
}

func (p *Preheater) key(oid, tid string) string {
	return oid + "|" + tid
}

func (p *Preheater) Take(oid, tid string) *websocket.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := p.key(oid, tid)
	pc := p.ready[k]
	if pc == nil {
		return nil
	}
	delete(p.ready, k)
	return pc.conn
}

func (p *Preheater) Warm(ctx context.Context, oid, tid, wsURL string) {
	conn, resp, err := p.dialer.DialContext(ctx, wsURL, p.header.Clone())
	if err != nil {
		if resp != nil {
			log.Printf("[preheat] dial failed oid=%s status=%d", oid, resp.StatusCode)
		} else {
			log.Printf("[preheat] dial failed oid=%s err=%v", oid, err)
		}
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"protocol":"json","version":1}`+"\x1e")); err != nil {
		conn.Close()
		log.Printf("[preheat] handshake send failed oid=%s err=%v", oid, err)
		return
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		conn.Close()
		log.Printf("[preheat] handshake recv failed oid=%s err=%v", oid, err)
		return
	}
	p.mu.Lock()
	k := p.key(oid, tid)
	if old, ok := p.ready[k]; ok {
		old.conn.Close()
	}
	p.ready[k] = &preheatedConn{conn: conn, created: time.Now()}
	p.mu.Unlock()
	log.Printf("[preheat] ready oid=%s tid=%s", oid, tid)
	go p.scheduleRefresh(oid, tid, wsURL)
}

func (p *Preheater) scheduleRefresh(oid, tid, wsURL string) {
	time.Sleep(3 * time.Minute)
	p.mu.Lock()
	_, exists := p.ready[p.key(oid, tid)]
	p.mu.Unlock()
	if !exists {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p.Warm(ctx, oid, tid, wsURL)
}

func (p *Preheater) gc() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		p.mu.Lock()
		for k, pc := range p.ready {
			if time.Since(pc.created) > 5*time.Minute {
				pc.conn.Close()
				delete(p.ready, k)
				log.Printf("[preheat] gc expired key=%s", k)
			}
		}
		p.mu.Unlock()
	}
}

func (p *Preheater) Stats() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{"ready_connections": len(p.ready), "enabled": p.enabled}
}
