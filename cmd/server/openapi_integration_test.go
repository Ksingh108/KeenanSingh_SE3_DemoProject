//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gringottsbankbranchbooking/internal/config"
	"github.com/gringottsbankbranchbooking/internal/database"
	"github.com/gringottsbankbranchbooking/internal/domain"
	"github.com/gringottsbankbranchbooking/internal/handler"
	"github.com/gringottsbankbranchbooking/internal/health"
	"github.com/gringottsbankbranchbooking/internal/metrics"
	"github.com/gringottsbankbranchbooking/internal/middleware"
	"github.com/gringottsbankbranchbooking/internal/repository/postgres"
	"github.com/gringottsbankbranchbooking/internal/service"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRouterResponsesMatchOpenAPI(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
		tcpostgres.WithDatabase("gringotts_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get postgres connection string: %v", err)
	}
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres connection string: %v", err)
	}
	password, _ := parsedDSN.User.Password()
	db, err := database.NewConnection(&config.DatabaseConfig{
		Host: parsedDSN.Hostname(), Port: parsedDSN.Port(),
		User: parsedDSN.User.Username(), Password: password,
		Name: strings.TrimPrefix(parsedDSN.Path, "/"), SSLMode: "disable",
		MaxOpenConns: 10, MaxIdleConns: 5,
		LockTimeout: time.Second, StatementTimeout: 3 * time.Second,
	}, "demo")
	if err != nil {
		t.Fatalf("connect and migrate postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close postgres: %v", err)
		}
	})

	bookingRepo := postgres.NewBookingRepository(db)
	branchRepo := postgres.NewBranchRepository(db)
	adminActionRepo := postgres.NewAdminActionRepository(db)
	serviceRepo := postgres.NewServiceRepository(db)
	sessionRepo := postgres.NewSessionRepository(db)
	authService := service.NewAuthService(postgres.NewAdminRepository(db), sessionRepo)
	adminHandler := handler.NewAdminHandler(authService)
	t.Cleanup(adminHandler.Stop)
	requestMetrics := metrics.New(db)
	router := chi.NewRouter()
	router.Use(middleware.RequestObservability(slog.New(slog.NewTextHandler(io.Discard, nil)), requestMetrics))
	router.Use(chimiddleware.Recoverer)
	router.Use(middleware.RequestDeadline(5 * time.Second))
	router.Use(middleware.SecurityHeaders)
	registerRoutes(
		router,
		handler.NewBookingHandler(service.NewBookingService(bookingRepo, branchRepo), service.NewAdminActionService(adminActionRepo)),
		handler.NewBranchHandler(service.NewBranchService(branchRepo), service.NewAdminActionService(adminActionRepo)),
		handler.NewBranchServiceHandler(service.NewServiceService(serviceRepo)),
		adminHandler,
		health.NewHealthChecker(db),
		requestMetrics.Handler(),
		middleware.RequireAuth(authService),
	)

	specPath := filepath.Join("api", "openapi.yaml")
	spec, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load OpenAPI spec: %v", err)
	}
	if err := spec.Validate(ctx); err != nil {
		t.Fatalf("validate OpenAPI spec: %v", err)
	}
	specRouter, err := legacy.NewRouter(spec)
	if err != nil {
		t.Fatalf("build OpenAPI router: %v", err)
	}

	requestNumber := 0
	serve := func(method, path, contentType, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		requestNumber++
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", requestNumber)
		request.Header.Set("Accept", "application/json")
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		validateOpenAPIResponse(t, specRouter, request, response)
		return response
	}

	branchID := "550e8400-e29b-41d4-a716-446655440001"
	serviceID := "660e8400-e29b-41d4-a716-446655440001"
	date := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	bookingBody := fmt.Sprintf(`{"branch_id":%q,"service_id":%q,"customer_name":"Jane Smith","email":"jane@example.com","phone":"+27 21 555 0100","date":%q,"time_slot":"09:00"}`, branchID, serviceID, date)
	if _, err := db.ExecContext(ctx, `UPDATE branches SET capacity = 1 WHERE id = $1`, branchID); err != nil {
		t.Fatalf("set branch capacity: %v", err)
	}

	created := serve(http.MethodPost, "/api/bookings", "application/json", bookingBody, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create booking = %d: %s", created.Code, created.Body)
	}
	serve(http.MethodPost, "/api/bookings", "application/json", `{"branch_id":"invalid"}`, nil)
	unknownBranchBody := strings.Replace(bookingBody, branchID, "550e8400-e29b-41d4-a716-446655440099", 1)
	serve(http.MethodPost, "/api/bookings", "application/json", unknownBranchBody, nil)
	fullSlot := serve(http.MethodPost, "/api/bookings", "application/json", bookingBody, nil)
	if fullSlot.Code != http.StatusConflict {
		t.Fatalf("booking full slot = %d, want 409: %s", fullSlot.Code, fullSlot.Body)
	}
	serve(http.MethodGet, "/api/availability/"+branchID+"/"+date, "", "", nil)
	serve(http.MethodGet, "/api/branches", "", "", nil)
	serve(http.MethodGet, "/api/services", "", "", nil)
	serve(http.MethodGet, "/live", "", "", nil)
	serve(http.MethodGet, "/ready", "", "", nil)
	serve(http.MethodGet, "/metrics", "", "", nil)

	unauthorized := serve(http.MethodGet, "/api/bookings", "", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated booking list = %d, want 401", unauthorized.Code)
	}
	loginFailure := serve(http.MethodPost, "/admin/login", "application/json", `{"username":"admin","password":"invalid"}`, nil)
	if loginFailure.Code != http.StatusUnauthorized {
		t.Fatalf("invalid admin login = %d, want 401: %s", loginFailure.Code, loginFailure.Body)
	}
	var adminID string
	if err := db.QueryRowContext(ctx, `SELECT id FROM admins WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatalf("find seeded admin: %v", err)
	}
	token := "openapi-contract-test-session"
	tokenHash := sha256.Sum256([]byte(token))
	if err := sessionRepo.Create(ctx, &domain.AdminSession{
		TokenHash: hex.EncodeToString(tokenHash[:]),
		AdminID:   adminID,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create contract-test session: %v", err)
	}
	adminCookie := &http.Cookie{Name: "admin_token", Value: token}
	list := serve(http.MethodGet, "/api/bookings", "", "", adminCookie)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"date":"`+date+`"`) {
		t.Fatalf("booking list = %d, expected YYYY-MM-DD booking date %q: %s", list.Code, date, list.Body)
	}
	serve(http.MethodGet, "/api/bookings?limit=invalid", "", "", adminCookie)
	serve(http.MethodDelete, "/api/bookings/not-a-uuid", "", "", adminCookie)
	serve(http.MethodPost, "/admin/logout", "", "", adminCookie)
}

func validateOpenAPIResponse(t *testing.T, specRouter routers.Router, request *http.Request, response *httptest.ResponseRecorder) {
	t.Helper()
	// httptest requests have a path-only URL (no scheme or host), so the spec
	// router cannot match them to any entry under `servers:` in openapi.yaml.
	// Give a copy of the request the host of the development server entry
	// (http://localhost:8080). Only this lookup uses the copy; the request served
	// by the app router is unchanged.
	matchRequest := request.Clone(request.Context())
	matchRequest.URL.Scheme = "http"
	matchRequest.URL.Host = "localhost:8080"
	route, pathParams, err := specRouter.FindRoute(matchRequest)
	if err != nil {
		t.Fatalf("find OpenAPI operation for %s %s: %v", request.Method, request.URL, err)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    request,
			PathParams: pathParams,
			Route:      route,
		},
		Status: response.Code,
		Header: response.Header(),
		Body:   io.NopCloser(strings.NewReader(response.Body.String())),
	}
	if err := openapi3filter.ValidateResponse(request.Context(), input); err != nil {
		t.Errorf("OpenAPI validation failed for %s %s (%d): %v\nresponse: %s", request.Method, request.URL, response.Code, err, response.Body.String())
	}
}
