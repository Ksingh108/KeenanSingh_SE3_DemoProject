// Package main provides the entry point for the Gringotts Bank Branch Booking System server.
// This application is layered (handler, service, repository) with PostgreSQL persistence,
// secure HTTP handlers, and graceful shutdown capabilities.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gringottsbankbranchbooking/internal/config"
	"github.com/gringottsbankbranchbooking/internal/database"
	"github.com/gringottsbankbranchbooking/internal/handler"
	"github.com/gringottsbankbranchbooking/internal/health"
	"github.com/gringottsbankbranchbooking/internal/metrics"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/postgres"
	"github.com/gringottsbankbranchbooking/internal/service"
	"github.com/gringottsbankbranchbooking/pkg/logger"
)

// main initializes and starts the Gringotts Bank Branch Booking System server.
// It sets up the complete application stack including:
// - Configuration loading and validation
// - PostgreSQL database connection with proper cleanup
// - Repository layer initialization (data access)
// - Service layer initialization (business logic)
// - HTTP handlers and middleware setup
// - Graceful shutdown with signal handling
//
// The server is layered, with a clear separation of concerns:
// Domain -> Repository -> Service -> Handler layers.
func main() {
	slog.SetDefault(slog.New(middleware.NewContextHandler(logger.New(os.Stderr).Handler())))
	if err := run(); err != nil {
		slog.Error("Operation failed", "operation", "server", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	// Load application configuration from environment variables and defaults
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	// Establish PostgreSQL database connection with connection pooling using loaded config
	slog.Info("Connecting to PostgreSQL database", "host", cfg.Database.Host)
	db, err := database.NewConnection(&cfg.Database, cfg.AppEnvironment)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	// Ensure database connection is properly closed on application shutdown.
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close database connection", "error", err.Error())
		}
	}()

	if cfg.AppEnvironment == "production" {
		if err := database.RequireNonDemoAdmin(context.Background(), db); err != nil {
			return err
		}
	}

	// Initialize repository layer - handles data persistence and retrieval
	// Each repository implements the corresponding interface from repository package
	bookingRepo := postgres.NewBookingRepository(db) // Manages booking data operations
	branchRepo := postgres.NewBranchRepository(db)   // Manages branch data operations
	adminActionRepo := postgres.NewAdminActionRepository(db)
	serviceRepo := postgres.NewServiceRepository(db) // Manages service data operations
	adminRepo := postgres.NewAdminRepository(db)     // Manages admin authentication data
	sessionRepo := postgres.NewSessionRepository(db) // Manages admin session data

	// Initialize service layer - contains business logic and orchestrates repository operations
	// Services implement domain rules and validation before delegating to repositories
	bookingService := service.NewBookingService(bookingRepo, branchRepo) // Handles booking business logic
	branchService := service.NewBranchService(branchRepo)                // Handles branch operations
	authService := service.NewAuthService(adminRepo, sessionRepo)        // Handles admin authentication
	cleanupService := service.NewCleanupService(sessionRepo)             // Handles expired session cleanup
	serviceService := service.NewServiceService(serviceRepo)             // Handles service operations
	adminActionService := service.NewAdminActionService(adminActionRepo)

	// Start background cleanup service for expired admin sessions
	// This prevents session table from growing indefinitely
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go cleanupService.StartPeriodicCleanup(cleanupCtx)
	defer cleanupCancel() // Ensure cleanup service stops on application shutdown

	// Initialize HTTP handlers - handle HTTP requests and responses
	// Handlers validate input, call services, and format responses
	bookingHandler := handler.NewBookingHandler(bookingService, adminActionService) // Handles booking API endpoints
	branchHandler := handler.NewBranchHandler(branchService, adminActionService)    // Handles branch API endpoints
	branchServiceHandler := handler.NewBranchServiceHandler(serviceService)         // Handles banking service API endpoints
	adminHandler := handler.NewAdminHandler(authService)                            // Handles admin operations
	defer adminHandler.Stop()
	healthChecker := health.NewHealthChecker(db) // Handles health check endpoints
	requestMetrics := metrics.New(db)

	// Setup router
	r := chi.NewRouter()
	requestTimeout := cfg.Server.WriteTimeout
	if requestTimeout > time.Second {
		requestTimeout -= time.Second
	}
	requestTimeout = min(requestTimeout, 5*time.Second)

	r.Use(middleware.RequestObservability(slog.Default(), requestMetrics))
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.RequestDeadline(requestTimeout))
	r.Use(middleware.SecurityHeaders)

	// Configure CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Security.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Idempotency-Key"},
		ExposedHeaders:   []string{"Link", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	registerRoutes(r, bookingHandler, branchHandler, branchServiceHandler, adminHandler, healthChecker, requestMetrics.Handler(), middleware.RequireAuth(authService))

	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	slog.Info("Server starting", "port", cfg.Server.Port, "database", "PostgreSQL")

	// Graceful shutdown
	serverErrors := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- fmt.Errorf("server failed: %w", err)
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(c)
	select {
	case <-c:
	case err := <-serverErrors:
		return err
	}

	slog.Info("Shutting down server")
	cleanupCancel() // Stop cleanup service
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shut down server: %w", err)
	}
	slog.Info("Server stopped")
	return nil
}

func registerRoutes(
	r chi.Router,
	bookingHandler *handler.BookingHandler,
	branchHandler *handler.BranchHandler,
	branchServiceHandler *handler.BranchServiceHandler,
	adminHandler *handler.AdminHandler,
	healthChecker *health.HealthChecker,
	metricsHandler http.Handler,
	requireAuth func(http.Handler) http.Handler,
) {
	r.Get("/live", health.LivenessHandler)
	r.Get("/health", healthChecker.HealthHandler)
	r.Get("/ready", healthChecker.ReadinessHandler)
	r.Method(http.MethodGet, "/metrics", metricsHandler)

	r.Route("/api", func(r chi.Router) {
		r.Post("/bookings", bookingHandler.CreateBooking)
		r.With(requireAuth).Get("/bookings", bookingHandler.GetBookings)
		r.With(requireAuth).Delete("/bookings/{id}", bookingHandler.CancelBooking)
		r.Get("/availability/{branchId}/{date}", bookingHandler.CheckAvailability)
		r.Get("/branches", branchHandler.GetBranches)
		r.Get("/services", branchServiceHandler.GetServices)
	})

	r.Route("/admin", func(r chi.Router) {
		r.Post("/login", adminHandler.Login)
		r.Post("/logout", adminHandler.Logout)
		r.With(requireAuth).Get("/", adminHandler.AdminPage)
		r.With(requireAuth).Get("/branches", adminHandler.AdminBranchesPage)
		r.With(requireAuth).Get("/bookings", adminHandler.AdminBookingsPage)
		r.With(requireAuth).Post("/branches", branchHandler.CreateBranch)
		r.With(requireAuth).Delete("/branches/{id}", branchHandler.DeleteBranch)
	})

	registerStaticRoutes(r)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/index.html")
	})
	r.Get("/booking", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/booking.html")
	})
	r.Get("/branches", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/branches.html")
	})
}

func registerStaticRoutes(router chi.Router) {
	router.HandleFunc("/static/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/style.css")
	})
	router.HandleFunc("/static/alpine.min.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/alpine.min.js")
	})
}
