package hostbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	nfs "github.com/willscott/go-nfs"
	helpers "github.com/willscott/go-nfs/helpers"
)

// Server has no pathname-based fallback. Every export owns one opened directory
// until Release/Close; its unguessable export and file handles are capabilities.
// The listener is loopback-only, including when Docker Desktop proxies it.
type Server struct {
	mu       sync.RWMutex
	exports  map[string]*rootFS
	closed   bool
	listener *boundedListener
	done     chan struct{}
}

func New(ctx context.Context) (*Server, error) {
	raw, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	s := &Server{exports: map[string]*rootFS{}, listener: &boundedListener{Listener: raw, connections: map[*boundedConn]bool{}}, done: make(chan struct{})}
	dispatcher := &dispatch{Handler: helpers.NewNullAuthHandler(memfs.New()), server: s}
	cache := helpers.NewCachingHandler(dispatcher, 8192)
	handler := &nilSafeHandle{Handler: cache}
	srv := &nfs.Server{Handler: handler, Context: ctx}
	go func() { defer close(s.done); _ = srv.Serve(s.listener) }()
	return s, nil
}
func (s *Server) Port() int { return s.listener.Addr().(*net.TCPAddr).Port }
func (s *Server) Export(root *os.Root, readOnly bool, uid, gid uint32) (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", os.ErrClosed
	}
	if len(s.exports) >= 64 {
		return "", errors.New("bridge export budget reached")
	}
	select {
	case <-s.done:
		return "", os.ErrClosed
	default:
	}
	s.exports[token] = &rootFS{root: root, ro: readOnly, uid: uid, gid: gid, files: map[*rootFile]bool{}}
	return token, nil
}
func (s *Server) Release(token string) {
	s.mu.Lock()
	fs := s.exports[token]
	delete(s.exports, token)
	s.mu.Unlock()
	if fs != nil {
		fs.close()
	}
}
func (s *Server) Close() {
	s.listener.Close()
	s.mu.Lock()
	s.closed = true
	all := s.exports
	s.exports = map[string]*rootFS{}
	s.mu.Unlock()
	for _, fs := range all {
		fs.close()
	}
	<-s.done
}

type dispatch struct {
	nfs.Handler
	server *Server
}

func (d *dispatch) Mount(_ context.Context, _ net.Conn, r nfs.MountRequest) (nfs.MountStatus, billy.Filesystem, []nfs.AuthFlavor) {
	p := string(r.Dirpath)
	if len(p) != 65 || p[0] != '/' {
		return nfs.MountStatusErrAcces, nil, nil
	}
	d.server.mu.RLock()
	fs := d.server.exports[p[1:]]
	d.server.mu.RUnlock()
	if fs == nil {
		return nfs.MountStatusErrAcces, nil, nil
	}
	return nfs.MountStatusOk, fs, []nfs.AuthFlavor{nfs.AuthFlavorNull}
}
func (d *dispatch) Change(f billy.Filesystem) billy.Change {
	if fs, ok := f.(*rootFS); ok {
		return fs
	}
	return nil
}

type nilSafeHandle struct{ nfs.Handler }

func (h *nilSafeHandle) ToHandle(f billy.Filesystem, p []string) []byte {
	if f == nil {
		return nil
	}
	return h.Handler.ToHandle(f, p)
}

type boundedListener struct {
	net.Listener
	mu          sync.Mutex
	connections map[*boundedConn]bool
	closed      bool
}
type boundedConn struct {
	net.Conn
	owner *boundedListener
	once  sync.Once
}

func (l *boundedListener) Accept() (net.Conn, error) {
	for {
		raw, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		l.mu.Lock()
		if l.closed || len(l.connections) >= 16 {
			l.mu.Unlock()
			raw.Close()
			continue
		}
		c := &boundedConn{Conn: raw, owner: l}
		l.connections[c] = true
		l.mu.Unlock()
		return c, nil
	}
}
func (l *boundedListener) Close() error {
	e := l.Listener.Close()
	l.mu.Lock()
	l.closed = true
	all := make([]*boundedConn, 0, len(l.connections))
	for c := range l.connections {
		all = append(all, c)
	}
	l.mu.Unlock()
	for _, c := range all {
		c.Close()
	}
	return e
}
func (c *boundedConn) Close() error {
	var e error
	c.once.Do(func() { e = c.Conn.Close(); c.owner.mu.Lock(); delete(c.owner.connections, c); c.owner.mu.Unlock() })
	return e
}
func (c *boundedConn) Read(b []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(time.Minute))
	n, e := c.Conn.Read(b)
	if e != nil {
		c.Close()
	}
	return n, e
}
func (c *boundedConn) Write(b []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	n, e := c.Conn.Write(b)
	if e != nil {
		c.Close()
	}
	return n, e
}
