package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestIntrospector(t *testing.T, handler http.HandlerFunc) *HTTPIntrospector {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	i, err := NewHTTPIntrospector(server.URL+"/", server.URL+"/oauth2/introspect", "baobab-control-plane", "synthetic-introspection-secret")
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Timeout = 5 * time.Second
	client.CheckRedirect = i.Client.CheckRedirect
	i.Client = client
	return i
}

func TestNewHTTPIntrospectorRefusesUnsafeConfiguration(t *testing.T) {
	for name, c := range map[string][4]string{
		"http endpoint":    {"https://iam.example.test/", "http://iam.example.test/introspect", "cp", "synthetic-introspection-secret"},
		"other origin":     {"https://iam.example.test/", "https://attacker.example.test/introspect", "cp", "synthetic-introspection-secret"},
		"query":            {"https://iam.example.test/", "https://iam.example.test/introspect?x=1", "cp", "synthetic-introspection-secret"},
		"credentials":      {"https://iam.example.test/", "https://u:p@iam.example.test/introspect", "cp", "synthetic-introspection-secret"},
		"http issuer":      {"http://iam.example.test/", "https://iam.example.test/introspect", "cp", "synthetic-introspection-secret"},
		"short secret":     {"https://iam.example.test/", "https://iam.example.test/introspect", "cp", "short"},
		"missing client":   {"https://iam.example.test/", "https://iam.example.test/introspect", "", "synthetic-introspection-secret"},
		"spaced client id": {"https://iam.example.test/", "https://iam.example.test/introspect", "c p", "synthetic-introspection-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewHTTPIntrospector(c[0], c[1], c[2], c[3]); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}

func TestHTTPIntrospectorReadsAnActiveToken(t *testing.T) {
	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodPost || !ok || user != "baobab-control-plane" || pass != "synthetic-introspection-secret" ||
			r.FormValue("token") != "synthetic.token.value" || r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true,"iss":"https://iam.example.test/","sub":"s","client_id":"zuribeans-bff","aud":["baobab-trade"],"scope":"openid trade:buy","exp":4102444800}`))
	})
	got, err := i.Introspect(context.Background(), "synthetic.token.value")
	if err != nil || !got.Active || got.Subject != "s" || got.ClientID != "zuribeans-bff" || got.Audience[0] != "baobab-trade" ||
		!got.ExpiresAt.Equal(time.Unix(4102444800, 0).UTC()) {
		t.Fatalf("introspection = %+v, %v", got, err)
	}
	if _, ok := got.Scopes["trade:buy"]; !ok {
		t.Fatal("scope not read")
	}
}

func TestHTTPIntrospectorSeparatesInactiveFromUnavailable(t *testing.T) {
	inactive := newTestIntrospector(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false}`))
	})
	if got, err := inactive.Introspect(context.Background(), "t"); err != nil || got.Active {
		t.Fatalf("inactive = %+v, %v", got, err)
	}
	for name, handler := range map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://attacker.example.test/", http.StatusFound)
		},
		"not json": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`{"active":true}`))
		},
		"oversized": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"active":true,"sub":"` + strings.Repeat("a", maximumIntrospectionResponse) + `"}`))
		},
		"bad audience": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"active":true,"aud":7}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			i := newTestIntrospector(t, handler)
			if _, err := i.Introspect(context.Background(), "t"); !errors.Is(err, ErrIntrospectionUnavailable) {
				t.Fatalf("err = %v, want unavailable", err)
			}
		})
	}
}
