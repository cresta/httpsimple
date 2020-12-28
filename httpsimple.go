package httpserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/cresta/gotracing"
	"github.com/cresta/zapctx"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

func HealthHandler(z *zapctx.Logger, tracer gotracing.Tracing) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Note: I may need to eventually abstarct this per tracing handler
		tracer.AttachTag(req.Context(), "sampling.priority", 0)
		_, err := io.WriteString(rw, "OK")
		z.IfErr(err).Warn(req.Context(), "unable to write back health response")
	})
}

type CanHTTPWrite interface {
	HTTPWrite(ctx context.Context, w http.ResponseWriter, l *zapctx.Logger)
}

type BasicResponse struct {
	Code    int
	Msg     io.WriterTo
	Headers map[string]string
}

func (g *BasicResponse) HTTPWrite(ctx context.Context, w http.ResponseWriter, l *zapctx.Logger) {
	for k, v := range g.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(g.Code)
	if w != nil {
		_, err := g.Msg.WriteTo(w)
		l.IfErr(err).Error(ctx, "unable to write final object")
	}
}

func BasicHandler(handler func(request *http.Request) CanHTTPWrite, l *zapctx.Logger) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		handler(request).HTTPWrite(request.Context(), writer, l)
	})
}

func LogMiddleware(logger *zapctx.Logger, filterFunc func(req *http.Request) bool) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			start := time.Now()
			defer func() {
				if !filterFunc(request) {
					logger.Info(request.Context(), "end request", zap.Duration("total_time", time.Since(start)))
				}
			}()
			handler.ServeHTTP(writer, request)
		})
	}
}

func MuxMiddleware() func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			r := mux.CurrentRoute(request)
			if r != nil {
				for k, v := range mux.Vars(request) {
					request = request.WithContext(zapctx.With(request.Context(), zap.String(fmt.Sprintf("mux.vars.%s", k), v)))
				}
				if r.GetName() != "" {
					request = request.WithContext(zapctx.With(request.Context(), zap.String("mux.name", r.GetName())))
				}
			}
			handler.ServeHTTP(writer, request)
		})
	}
}

func NotFoundHandler(logger *zapctx.Logger) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		logger.With(zap.String("handler", "not_found"), zap.String("url", req.URL.String())).Warn(req.Context(), "unknown request")
		http.NotFoundHandler().ServeHTTP(rw, req)
	})
}

func BasicServerRun(logger *zapctx.Logger, server *http.Server, onListen func(listener net.Listener), addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if onListen != nil {
		onListen(ln)
	}

	logger.Info(context.Background(), "starting server")
	serveErr := server.Serve(ln)
	if serveErr != http.ErrServerClosed {
		return serveErr
	}
	logger.Info(context.Background(), "Server finished")
	return nil
}
