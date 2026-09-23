package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		"":               "http://127.0.0.1:8080/healthz",
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"[::]:9000":      "http://127.0.0.1:9000/healthz",
		"10.0.0.5:8080":  "http://10.0.0.5:8080/healthz",
		"localhost:8081": "http://localhost:8081/healthz",
	}
	for address, want := range cases {
		got, err := healthURL(address)
		if err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", address, got, err, want)
		}
	}
	if _, err := healthURL("8080"); err == nil {
		t.Error("healthURL accepted an address without a port separator")
	}
}

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")

	if code := healthcheck(address); code != 0 {
		t.Fatalf("healthy server: exit %d, want 0", code)
	}
	status = http.StatusServiceUnavailable
	if code := healthcheck(address); code != 1 {
		t.Fatalf("unhealthy server: exit %d, want 1", code)
	}
	server.Close()
	if code := healthcheck(address); code != 1 {
		t.Fatalf("unreachable server: exit %d, want 1", code)
	}
}
