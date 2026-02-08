package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"github.com/velum/internal/api/handlers"
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

	// Health check endpoint
	r.Get("/health", s.handler.Health)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/analyze", s.handler.Analyze)
	})

	return r
}
