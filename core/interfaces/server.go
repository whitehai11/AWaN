package interfaces

import (
	"context"
	"net/http"
	"time"

	"github.com/awan/awan/core/runtime"
)

// Server hosts the local runtime API.
type Server struct {
	runtime *runtime.Runtime
	api     *API
}

// NewServer creates a local API server.
func NewServer(rt *runtime.Runtime) *Server {
	return &Server{
		runtime: rt,
		api:     NewAPI(rt),
	}
}

// Start begins serving the local API until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.runtime.Config().Address(),
		Handler:           s.api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.runtime.Logger().Log("AWAN", "Runtime started")
	s.runtime.Logger().Log("AWAN", "API listening on "+s.runtime.Config().Address())

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
