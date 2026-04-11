package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"github.com/velum/internal/api/handlers"
	securitymw "github.com/velum/internal/api/middleware"
	"github.com/velum/internal/config"
)

// Server represents the API server
type Server struct {
	cfg     *config.Config
	handler *handlers.Handler
}

// NewServer creates a new API server instance
func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:     cfg,
		handler: handlers.NewHandler(cfg),
	}
}

// NewDemoServer creates a server backed entirely by in-memory storage.
// No database, no LLM, no config file required.
func NewDemoServer(cfg *config.Config) *Server {
	return &Server{
		cfg:     cfg,
		handler: handlers.NewDemoHandler(),
	}
}

// Shutdown releases all resources held by the server (DB connections, background
// goroutines). It should be called after the HTTP server has stopped accepting
// new requests.
func (s *Server) Shutdown() error {
	return s.handler.Close()
}

// Router returns the configured chi router
func (s *Server) Router() *chi.Mux {
	r := chi.NewRouter()

	// CORS middleware
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CORS.AllowedOrigins,
		AllowedMethods:   s.cfg.CORS.AllowedMethods,
		AllowedHeaders:   s.cfg.CORS.AllowedHeaders,
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any major browser
	}))

	// Rate limiting middleware
	if s.cfg.Resiliency.RateLimitRequests > 0 {
		r.Use(httprate.Limit(
			s.cfg.Resiliency.RateLimitRequests,
			time.Second,
			httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"success":false,"message":"Rate limit exceeded. Please slow down."}`))
			}),
		))
	}

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check endpoint — NOT behind security middleware so load balancers
	// and k8s probes can reach it without an API key.
	r.Get("/health", s.handler.Health)

	// Authenticated routes — security middleware applies to this group only.
	r.Group(func(r chi.Router) {
		r.Use(securitymw.SecurityMiddleware(s.cfg))

		// API routes
		r.Route("/api/v1", func(r chi.Router) {
			r.Post("/analyze", s.handler.Analyze)
			r.Post("/baseline", s.handler.Baseline)
		})
	})

	return r
}
