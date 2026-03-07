package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/batuhan/easymatrix/internal/config"
	"github.com/batuhan/easymatrix/internal/gomuksruntime"
)

func TestHandlerUsesV1RoutesOnly(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		AccessToken:         "test-token",
		BeeperHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	for _, path := range []string{"/v0/get-accounts", "/v0/search", "/v0/spec", "/ws"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d, expected 404", path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("/v1/ws should remain registered")
	}
}

func TestOAuthProtectedResourceRejectsLegacyV0(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		AccessToken:         "test-token",
		BeeperHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/v0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/.well-known/oauth-protected-resource/v0 returned %d, expected 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/v1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("/.well-known/oauth-protected-resource/v1 should remain registered")
	}
}
