package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/gringottsbankbranchbooking/internal/handler"
	"github.com/gringottsbankbranchbooking/internal/health"
	"gopkg.in/yaml.v3"
)

type contractDocument struct {
	OpenAPI    string                           `yaml:"openapi"`
	Security   []map[string][]string            `yaml:"security"`
	Paths      map[string]map[string]contractOp `yaml:"paths"`
	Components struct {
		Parameters      map[string]contractParameter `yaml:"parameters"`
		SecuritySchemes map[string]struct {
			Type string `yaml:"type"`
			In   string `yaml:"in"`
			Name string `yaml:"name"`
		} `yaml:"securitySchemes"`
		Schemas map[string]struct {
			Required   []string `yaml:"required"`
			Properties map[string]struct {
				Enum     []string `yaml:"enum"`
				Type     string   `yaml:"type"`
				Format   string   `yaml:"format"`
				Pattern  string   `yaml:"pattern"`
				Minimum  *int     `yaml:"minimum"`
				Maximum  *int     `yaml:"maximum"`
				Required []string `yaml:"required"`
				Items    struct {
					Type   string `yaml:"type"`
					Format string `yaml:"format"`
				} `yaml:"items"`
				Properties map[string]struct {
					Type string `yaml:"type"`
				} `yaml:"properties"`
			} `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

type contractOp struct {
	Security   []map[string][]string       `yaml:"security"`
	Parameters []contractParameter         `yaml:"parameters"`
	Responses  map[string]contractResponse `yaml:"responses"`
}

type contractResponse struct {
	Description string `yaml:"description"`
	Content     map[string]struct {
		Schema struct {
			Ref string `yaml:"$ref"`
		} `yaml:"schema"`
	} `yaml:"content"`
}

type contractParameter struct {
	Ref    string `yaml:"$ref"`
	Name   string `yaml:"name"`
	In     string `yaml:"in"`
	Schema struct {
		Type    string `yaml:"type"`
		Format  string `yaml:"format"`
		Default *int   `yaml:"default"`
		Minimum *int   `yaml:"minimum"`
		Maximum *int   `yaml:"maximum"`
	} `yaml:"schema"`
}

func TestOpenAPIContract(t *testing.T) {
	document := loadOpenAPIDocument(t)
	if document.OpenAPI != "3.0.3" {
		t.Fatalf("OpenAPI version = %q, want 3.0.3", document.OpenAPI)
	}
	if len(document.Security) != 0 {
		t.Fatal("security must be operation-scoped; public endpoints must not inherit authentication")
	}

	cookie, ok := document.Components.SecuritySchemes["AdminSessionCookie"]
	if !ok || cookie.Type != "apiKey" || cookie.In != "cookie" || cookie.Name != "admin_token" {
		t.Fatalf("AdminSessionCookie scheme = %+v, want admin_token cookie authentication", cookie)
	}

	protected := []struct{ path, method string }{
		{"/api/bookings", "get"},
		{"/api/bookings/{id}", "delete"},
		{"/admin/", "get"},
		{"/admin/branches", "get"},
		{"/admin/branches", "post"},
		{"/admin/bookings", "get"},
		{"/admin/branches/{id}", "delete"},
	}
	for _, target := range protected {
		op := requireOperation(t, document, target.path, target.method)
		if len(op.Security) != 1 || len(op.Security[0]["AdminSessionCookie"]) != 0 {
			t.Errorf("%s %s must require AdminSessionCookie", target.method, target.path)
		}
	}

	public := []struct{ path, method string }{
		{"/api/bookings", "post"},
		{"/api/availability/{branchId}/{date}", "get"},
		{"/api/branches", "get"},
		{"/api/services", "get"},
		{"/admin/login", "post"},
		{"/admin/logout", "post"},
		{"/live", "get"},
		{"/health", "get"},
		{"/ready", "get"},
	}
	for _, target := range public {
		op := requireOperation(t, document, target.path, target.method)
		if len(op.Security) != 0 {
			t.Errorf("%s %s must remain public", target.method, target.path)
		}
	}

	availability := requireOperation(t, document, "/api/availability/{branchId}/{date}", "get").Responses["200"]
	if !strings.Contains(strings.ToLower(availability.Description), "at capacity") || !strings.Contains(strings.ToLower(availability.Description), "started today") {
		t.Errorf("availability response description = %q, want to document full and elapsed slots as unavailable", availability.Description)
	}
	availabilityBadRequest := requireOperation(t, document, "/api/availability/{branchId}/{date}", "get").Responses["400"]
	if !strings.Contains(strings.ToLower(availabilityBadRequest.Description), "before today") {
		t.Errorf("availability 400 description = %q, want to document rejection of past dates", availabilityBadRequest.Description)
	}

	for _, target := range []struct {
		path, method string
		statuses     []string
	}{
		{"/api/bookings", "post", []string{"201", "400", "404", "409", "413", "415", "429", "500", "503"}},
		{"/api/bookings", "get", []string{"200", "400", "401", "500", "503"}},
		{"/api/bookings/{id}", "delete", []string{"200", "400", "401", "404", "500", "503"}},
		{"/api/availability/{branchId}/{date}", "get", []string{"200", "400", "404", "500", "503"}},
		{"/api/branches", "get", []string{"200", "500", "503"}},
		{"/api/services", "get", []string{"200", "500", "503"}},
		{"/admin/login", "post", []string{"200", "400", "401", "413", "415", "429", "500", "503"}},
		{"/admin/logout", "post", []string{"200", "500", "503"}},
		{"/admin/", "get", []string{"302", "401"}},
		{"/admin/branches", "get", []string{"200", "401"}},
		{"/admin/branches", "post", []string{"201", "400", "401", "413", "415", "500", "503"}},
		{"/admin/bookings", "get", []string{"200", "401"}},
		{"/admin/branches/{id}", "delete", []string{"204", "400", "401", "404", "409", "500", "503"}},
	} {
		op := requireOperation(t, document, target.path, target.method)
		for _, status := range target.statuses {
			if _, ok := op.Responses[status]; !ok {
				t.Errorf("%s %s is missing response %s", target.method, target.path, status)
			}
		}
	}

	for _, target := range []struct{ path, method, status string }{
		{"/api/bookings", "post", "404"},
		{"/api/bookings", "post", "413"},
		{"/api/bookings", "post", "415"},
		{"/api/bookings", "post", "429"},
		{"/api/availability/{branchId}/{date}", "get", "404"},
		{"/api/branches", "get", "500"},
		{"/api/services", "get", "500"},
		{"/admin/login", "post", "400"},
		{"/admin/login", "post", "401"},
		{"/admin/login", "post", "413"},
		{"/admin/login", "post", "415"},
		{"/admin/login", "post", "429"},
		{"/admin/login", "post", "500"},
		{"/admin/logout", "post", "500"},
		{"/admin/branches", "post", "400"},
		{"/admin/branches", "post", "413"},
		{"/admin/branches", "post", "415"},
		{"/admin/branches", "post", "500"},
		{"/admin/branches/{id}", "delete", "400"},
		{"/admin/branches/{id}", "delete", "404"},
		{"/admin/branches/{id}", "delete", "500"},
	} {
		response := requireOperation(t, document, target.path, target.method).Responses[target.status]
		jsonContent, ok := response.Content["application/json"]
		if !ok || jsonContent.Schema.Ref != "#/components/schemas/Error" {
			t.Errorf("%s %s response %s must use the JSON Error schema", target.method, target.path, target.status)
		}
	}

	list := requireOperation(t, document, "/api/bookings", "get")
	assertPaginationParameter(t, document, list, "limit", 100, 1, 1000)
	assertPaginationParameter(t, document, list, "offset", 0, 0, 0)

	statuses := document.Components.Schemas["Booking"].Properties["status"].Enum
	if len(statuses) != 2 || statuses[0] != "confirmed" || statuses[1] != "cancelled" {
		t.Errorf("Booking status enum = %v, want [confirmed cancelled]", statuses)
	}

	createBooking := requireOperation(t, document, "/api/bookings", "post")
	if _, ok := createBooking.Responses["201"]; !ok {
		t.Error("POST /api/bookings must document 201 when a booking is created")
	}
	if _, ok := createBooking.Responses["200"]; ok {
		t.Error("POST /api/bookings must not document 200 for resource creation")
	}
	errorSchema := document.Components.Schemas["Error"].Properties["error"]
	if errorSchema.Type != "object" || errorSchema.Properties["code"].Type != "string" || errorSchema.Properties["message"].Type != "string" {
		t.Errorf("Error schema must provide a nested string code and message, got %+v", errorSchema)
	}
	if !contains(errorSchema.Required, "code") || !contains(errorSchema.Required, "message") {
		t.Errorf("Error schema must require code and message, got %v", errorSchema.Required)
	}
	if errorSchema.Properties["request_id"].Type != "string" {
		t.Error("Error schema must document request_id")
	}

	bookingTimePattern := document.Components.Schemas["CreateBookingRequest"].Properties["time_slot"].Pattern
	if bookingTimePattern != `^((09|1[0-6]):(00|30)|17:00)$` {
		t.Errorf("booking time_slot pattern = %q, want the 09:00-17:00 half-hour grid", bookingTimePattern)
	}
	responseBookingTimePattern := document.Components.Schemas["Booking"].Properties["time_slot"].Pattern
	if responseBookingTimePattern != bookingTimePattern {
		t.Errorf("Booking time_slot pattern = %q, want %q", responseBookingTimePattern, bookingTimePattern)
	}

	branchSchema := document.Components.Schemas["Branch"]
	for _, field := range []string{"id", "name", "address", "city", "phone", "hours", "capacity", "created_at"} {
		if !contains(branchSchema.Required, field) {
			t.Errorf("Branch schema must require %q", field)
		}
	}
	capacity := branchSchema.Properties["capacity"]
	if capacity.Minimum == nil || *capacity.Minimum != 1 || capacity.Maximum == nil || *capacity.Maximum != 1000 {
		t.Errorf("Branch capacity bounds = [%v, %v], want [1, 1000]", capacity.Minimum, capacity.Maximum)
	}

	for _, target := range []struct{ schema, field string }{
		{"CreateBookingRequest", "branch_id"},
		{"CreateBookingRequest", "service_id"},
		{"Booking", "id"},
		{"Booking", "branch_id"},
		{"Booking", "service_id"},
		{"Branch", "id"},
		{"Service", "id"},
	} {
		property := document.Components.Schemas[target.schema].Properties[target.field]
		if property.Type != "string" || property.Format != "uuid" {
			t.Errorf("%s.%s schema = (%q, %q), want string uuid", target.schema, target.field, property.Type, property.Format)
		}
	}
	serviceIDs := document.Components.Schemas["CreateBranchRequest"].Properties["services"].Items
	if serviceIDs.Type != "string" || serviceIDs.Format != "uuid" {
		t.Errorf("CreateBranchRequest.services items = (%q, %q), want string uuid", serviceIDs.Type, serviceIDs.Format)
	}

	for _, target := range []struct{ path, method, name string }{
		{"/api/bookings/{id}", "delete", "id"},
		{"/api/availability/{branchId}/{date}", "get", "branchId"},
		{"/admin/branches/{id}", "delete", "id"},
	} {
		op := requireOperation(t, document, target.path, target.method)
		found := false
		for _, parameter := range op.Parameters {
			if parameter.Name != target.name || parameter.In != "path" {
				continue
			}
			found = true
			if parameter.Schema.Type != "string" || parameter.Schema.Format != "uuid" {
				t.Errorf("%s %s path parameter %q = (%q, %q), want string uuid", target.method, target.path, target.name, parameter.Schema.Type, parameter.Schema.Format)
			}
		}
		if !found {
			t.Errorf("%s %s missing path parameter %q", target.method, target.path, target.name)
		}
	}
}

func TestOpenAPISpecIsValid(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load OpenAPI document: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI document: %v", err)
	}
}

func TestOpenAPIRoutesMatchChiRouter(t *testing.T) {
	document := loadOpenAPIDocument(t)
	router := chi.NewRouter()
	var bookingHandler *handler.BookingHandler
	var branchHandler *handler.BranchHandler
	var branchServiceHandler *handler.BranchServiceHandler
	var adminHandler *handler.AdminHandler
	var healthChecker *health.HealthChecker
	registerRoutes(router, bookingHandler, branchHandler, branchServiceHandler, adminHandler, healthChecker, http.NotFoundHandler(), func(next http.Handler) http.Handler {
		return next
	})

	documented := make(map[string]struct{})
	for path, operations := range document.Paths {
		for method := range operations {
			documented[strings.ToUpper(method)+" "+path] = struct{}{}
		}
	}

	actual := make(map[string]struct{})
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if isFrontendRoute(route) {
			return nil
		}
		actual[strings.ToUpper(method)+" "+route] = struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("walk chi routes: %v", err)
	}

	var differences []string
	for operation := range actual {
		if _, ok := documented[operation]; !ok {
			differences = append(differences, "chi route missing from OpenAPI: "+operation)
		}
	}
	for operation := range documented {
		if _, ok := actual[operation]; !ok {
			differences = append(differences, "OpenAPI operation missing from chi router: "+operation)
		}
	}
	sort.Strings(differences)
	for _, difference := range differences {
		t.Error(difference)
	}
}

func loadOpenAPIDocument(t *testing.T) contractDocument {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document contractDocument
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	return document
}

func isFrontendRoute(route string) bool {
	switch route {
	case "/", "/booking", "/branches", "/static/style.css", "/static/alpine.min.js":
		return true
	default:
		return false
	}
}

func requireOperation(t *testing.T, document contractDocument, path, method string) contractOp {
	t.Helper()
	methods, ok := document.Paths[path]
	if !ok {
		t.Fatalf("OpenAPI document is missing path %s", path)
	}
	op, ok := methods[method]
	if !ok {
		t.Fatalf("OpenAPI document is missing %s %s", method, path)
	}
	return op
}

func assertPaginationParameter(t *testing.T, document contractDocument, op contractOp, name string, defaultValue, minimum, maximum int) {
	t.Helper()
	for _, parameter := range op.Parameters {
		if parameter.Ref != "" {
			componentName := strings.TrimPrefix(parameter.Ref, "#/components/parameters/")
			if componentName == parameter.Ref {
				t.Errorf("unsupported parameter reference %q", parameter.Ref)
				return
			}
			var ok bool
			parameter, ok = document.Components.Parameters[componentName]
			if !ok {
				t.Errorf("unresolved parameter reference %q", componentName)
				return
			}
		}
		if parameter.Name != name || parameter.In != "query" {
			continue
		}
		if parameter.Schema.Default == nil || *parameter.Schema.Default != defaultValue {
			t.Errorf("%s default = %v, want %d", name, parameter.Schema.Default, defaultValue)
		}
		if parameter.Schema.Minimum == nil || *parameter.Schema.Minimum != minimum {
			t.Errorf("%s minimum = %v, want %d", name, parameter.Schema.Minimum, minimum)
		}
		if maximum != 0 && (parameter.Schema.Maximum == nil || *parameter.Schema.Maximum != maximum) {
			t.Errorf("%s maximum = %v, want %d", name, parameter.Schema.Maximum, maximum)
		}
		return
	}
	t.Errorf("missing query parameter %q", name)
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
