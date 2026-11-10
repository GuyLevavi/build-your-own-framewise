package internal

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
)

// dbProxy sits between the program and Postgres so a test can take the
// database away and give it back: down() drops every connection and refuses
// new ones, up() lets them through again. Nothing about the server changes.
type dbProxy struct {
	listener net.Listener
	upstream string
	mu       sync.Mutex
	isDown   bool
	conns    map[net.Conn]bool
}

// startDBProxy returns the proxy and dbURL rewritten to go through it.
func startDBProxy(dbURL string) (*dbProxy, string, error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return nil, "", err
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	p := &dbProxy{listener: listener, upstream: net.JoinHostPort(u.Hostname(), port), conns: map[net.Conn]bool{}}
	go p.serve()
	u.Host = listener.Addr().String()
	return p, u.String(), nil
}

func (p *dbProxy) serve() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		down := p.isDown
		p.mu.Unlock()
		if down {
			client.Close()
			continue
		}
		go p.pipe(client)
	}
}

func (p *dbProxy) pipe(client net.Conn) {
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		client.Close()
		return
	}
	p.mu.Lock()
	p.conns[client], p.conns[server] = true, true
	p.mu.Unlock()
	done := make(chan struct{}, 2)
	go func() { io.Copy(server, client); done <- struct{}{} }()
	go func() { io.Copy(client, server); done <- struct{}{} }()
	<-done
	client.Close()
	server.Close()
	p.mu.Lock()
	delete(p.conns, client)
	delete(p.conns, server)
	p.mu.Unlock()
}

func (p *dbProxy) down() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isDown = true
	for c := range p.conns {
		c.Close()
	}
}

func (p *dbProxy) up() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isDown = false
}

func (p *dbProxy) close() {
	p.down()
	p.listener.Close()
}

func (p *dbProxy) String() string {
	return fmt.Sprintf("proxy %s -> %s", p.listener.Addr(), p.upstream)
}
