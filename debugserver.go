package httpsimple

import (
	"context"
	"expvar"
	"fmt"
	"github.com/cresta/zapctx"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"sync/atomic"
)

type DebugServer struct {
	http.Server
	ListenAddr string
	Logger *zapctx.Logger
	listener atomic.Value
	mu sync.Mutex
}

func (d *DebugServer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.listener.Load()
	if l == nil {
		return nil
	}
	err := l.(net.Listener).Close()
	d.Logger.IfErr(err).Warn(context.Background(), "unable to close debug server")
	return err
}

func (d *DebugServer) Setup() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ListenAddr == "" || d.ListenAddr == "-" {
		return nil
	}
	m := http.NewServeMux()
	m.HandleFunc("/debug/pprof/", pprof.Index)
	m.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	m.HandleFunc("/debug/pprof/profile", pprof.Profile)
	m.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	m.HandleFunc("/debug/pprof/trace", pprof.Trace)
	m.Handle("/debug/vars", expvar.Handler())
	d.Handler = m

	ln, err := net.Listen("tcp", d.ListenAddr)
	if err != nil {
		return fmt.Errorf("unable to listen to %s: %w", d.ListenAddr, err)
	}
	d.listener.Store(ln)
	return nil
}

func (d *DebugServer) Start() error {
	l := d.listener.Load()
	if l == nil {
		d.Logger.Info(context.Background(), "no listen address set.  Not running debug server")
		return nil
	}
	serveErr := d.Serve(l.(net.Listener))
	if serveErr != http.ErrServerClosed {
		d.Logger.IfErr(serveErr).Error(context.Background(), "debug server existed")
	}
	d.Logger.Info(context.Background(), "debug server finished")
	return nil
}