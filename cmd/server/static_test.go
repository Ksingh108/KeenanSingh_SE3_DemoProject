package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestStaticRoutesAllowOnlyPublicAssets(t *testing.T) {
	root := t.TempDir()
	staticDir := filepath.Join(root, "web", "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"style.css", "alpine.min.js", "admin-bookings.html", "admin-branches.html", "input.css", "unexpected.html"} {
		if err := os.WriteFile(filepath.Join(staticDir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)

	router := chi.NewRouter()
	registerStaticRoutes(router)

	for _, asset := range []struct {
		path    string
		content string
	}{
		{path: "/static/style.css", content: "style.css"},
		{path: "/static/alpine.min.js", content: "alpine.min.js"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+asset.path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(method, asset.path, nil))
				if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
				}
				if method == http.MethodGet && response.Body.String() != asset.content {
					t.Errorf("body = %q, want %q", response.Body.String(), asset.content)
				}
			})
		}
	}

	for _, path := range []string{
		"/static",
		"/static/",
		"/static/admin-bookings.html",
		"/static/admin-branches.html",
		"/static/input.css",
		"/static/unexpected.html",
		"/static/%61dmin-bookings.html",
		"/static/../static/admin-branches.html",
		"/static/style.css/../admin-bookings.html",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", response.Code, http.StatusNotFound)
			}
		})
	}
}
