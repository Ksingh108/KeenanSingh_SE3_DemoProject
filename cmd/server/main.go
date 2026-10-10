// Package main provides the entry point for the Gringotts Bank Branch Booking System server.
// This application implements a Clean Architecture pattern with PostgreSQL persistence,
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

	"github.com/GringottsBankBranchBooking/internal/config"
	"github.com/GringottsBankBranchBooking/internal/database"
	"github.com/GringottsBankBranchBooking/internal/handler"
	"github.com/GringottsBankBranchBooking/internal/health"
	"github.com/GringottsBankBranchBooking/internal/middleware"
	"github.com/GringottsBankBranchBooking/internal/repository/postgres"
	"github.com/GringottsBankBranchBooking/internal/service"
	"github.com/GringottsBankBranchBooking/pkg/logger"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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
// The server follows Clean Architecture principles with clear separation of concerns:
// Domain -> Repository -> Service -> Handler layers.
func main() {
	slog.SetDefault(logger.New(os.Stderr))
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
	db, err := database.NewConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	// Ensure database connection is properly closed on application shutdown
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("Operation failed", "operation", "close database connection", "error", err.Error())
		}
	}()

	// Initialize repository layer - handles data persistence and retrieval
	// Each repository implements the corresponding interface from repository package
	bookingRepo := postgres.NewBookingRepository(db) // Manages booking data operations
	branchRepo := postgres.NewBranchRepository(db)   // Manages branch data operations
	serviceRepo := postgres.NewServiceRepository(db) // Manages service data operations
	adminRepo := postgres.NewAdminRepository(db)     // Manages admin authentication data
	sessionRepo := postgres.NewSessionRepository(db) // Manages admin session data
	auditRepo := postgres.NewAuditRepository(db)     // Manages the admin audit trail

	// Initialize service layer - contains business logic and orchestrates repository operations
	// Services implement domain rules and validation before delegating to repositories
	bookingService := service.NewBookingService(bookingRepo, branchRepo) // Handles booking business logic
	branchService := service.NewBranchService(branchRepo)                // Handles branch operations
	authService := service.NewAuthService(adminRepo, sessionRepo)        // Handles admin authentication
	cleanupService := service.NewCleanupService(sessionRepo)             // Handles expired session cleanup
	serviceService := service.NewServiceService(serviceRepo)             // Handles service operations
	auditService := service.NewAuditService(auditRepo)                   // Records privileged admin actions

	// Start background cleanup service for expired admin sessions
	// This prevents session table from growing indefinitely
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go cleanupService.StartPeriodicCleanup(cleanupCtx)
	defer cleanupCancel() // Ensure cleanup service stops on application shutdown

	// Initialize HTTP handlers - handle HTTP requests and responses
	// Handlers validate input, call services, and format responses
	bookingHandler := handler.NewBookingHandler(bookingService, auditService) // Handles booking API endpoints
	branchHandler := handler.NewBranchHandler(branchService, auditService)    // Handles branch API endpoints
	branchServiceHandler := handler.NewBranchServiceHandler(serviceService)   // Handles banking service API endpoints
	adminHandler := handler.NewAdminHandler(authService, branchService)       // Handles admin operations
	healthChecker := health.NewHealthChecker(db)                              // Handles health check endpoints

	// Setup router
	r := chi.NewRouter()

	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))
	r.Use(middleware.SecurityHeaders)

	// Configure CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Security.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check routes
	r.Get("/health", healthChecker.HealthHandler)
	r.Get("/ready", healthChecker.ReadinessHandler)

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Post("/bookings", bookingHandler.CreateBooking)
		// Admin-only routes: retrieving all bookings, getting bookings by email, and cancelling bookings.
		// These endpoints expose PII and allow DoS via arbitrary cancellation.
		// Protected by admin authentication via session token.
		r.With(middleware.RequireAuth(authService)).Get("/bookings", bookingHandler.GetBookings)
		r.With(middleware.RequireAuth(authService)).Get("/bookings/{email}", bookingHandler.GetBookingsByEmail)
		r.With(middleware.RequireAuth(authService)).Delete("/bookings/{id}", bookingHandler.CancelBooking)
		r.Get("/availability/{branchId}/{date}", bookingHandler.CheckAvailability)
		r.Get("/branches", branchHandler.GetBranches)
		r.Get("/services", branchServiceHandler.GetServices)
	})

	// Admin routes
	r.Route("/admin", func(r chi.Router) {
		r.Post("/login", adminHandler.Login)
		r.Post("/logout", adminHandler.Logout)
		r.With(middleware.RequireAuth(authService)).Get("/", adminHandler.AdminPage)
		r.With(middleware.RequireAuth(authService)).Get("/branches", adminHandler.AdminBranchesPage)
		r.With(middleware.RequireAuth(authService)).Get("/bookings", adminHandler.AdminBookingsPage)
		r.With(middleware.RequireAuth(authService)).Post("/branches", branchHandler.CreateBranch)
		r.With(middleware.RequireAuth(authService)).Delete("/branches/{id}", branchHandler.DeleteBranch)
	})

	// Serve static files
	fileServer := http.FileServer(http.Dir("./web/static"))
	r.Handle("/static/*", http.StripPrefix("/static", fileServer))
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/index.html")
	})
	r.Get("/booking", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/booking.html")
	})
	r.Get("/branches", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/static/branches.html")
	})

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
