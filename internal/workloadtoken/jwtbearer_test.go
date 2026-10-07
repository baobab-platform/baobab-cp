package workloadtoken

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const assertion = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJwcm92aXNpb25lciJ9.c2lnbmF0dXJl"

// hydra is a stand-in token endpoint that records every request it receives.
type hydra struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]string
	calls    atomic.Int32
	answer   func(w http.ResponseWriter, form map[string]string)
}

func newHydra(t *testing.T, answer func(w http.ResponseWriter, form map[string]string)) *hydra {
	t.Helper()
	h := &hydra{answer: answer}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = r.ParseForm()
		form := map[string]string{}
		for k, v := range r.PostForm {
			form[k] = v[0]
		}
		h.mu.Lock()
		h.requests = append(h.requests, form)
		h.mu.Unlock()
		h.answer(w, form)
	}))
	t.Cleanup(h.Close)
	return h
}

func ok(token string, expiresIn int, scope string) func(http.ResponseWriter, map[string]string) {
	return func(w http.ResponseWriter, _ map[string]string) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "token_type": "bearer", "expires_in": expiresIn, "scope": scope})
	}
}

func source(t *testing.T, h *hydra) (*JWTBearer, string, *time.Time) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "assertion")
	if err := os.WriteFile(file, []byte(assertion+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	return &JWTBearer{TokenURL: h.URL + "/oauth2/token", ClientID: "baobab-cp-provisioning-evidence-workload", AssertionFile: file,
		Scope: []string{"erp:provision"}, Now: func() time.Time { return now }}, file, &now
}

func TestExchangeSendsExactlyTheRFC7523Request(t *testing.T) {
	h := newHydra(t, ok("access-1", 900, "erp:provision"))
	s, _, _ := source(t, h)
	token, err := s.Token(context.Background())
	if err != nil || token != "access-1" {
		t.Fatalf("token %q err %v", token, err)
	}
	got := h.requests[0]
	want := map[string]string{"grant_type": GrantType, "assertion": assertion, "client_id": "baobab-cp-provisioning-evidence-workload", "scope": "erp:provision"}
	if len(got) != len(want) {
		t.Fatalf("request carried %v, want exactly %v (no secret, no audience unless configured)", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestAudienceIsSentOnlyWhenConfigured(t *testing.T) {
	h := newHydra(t, ok("a", 900, ""))
	s, _, _ := source(t, h)
	s.Audience = "baobab-erp"
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.requests[0]["audience"] != "baobab-erp" {
		t.Fatalf("audience not sent: %v", h.requests[0])
	}
}

func TestTokenIsCachedThenRefreshedBeforeItExpires(t *testing.T) {
	var n atomic.Int32
	h := newHydra(t, func(w http.ResponseWriter, f map[string]string) {
		ok("access-"+string(rune('0'+n.Add(1))), 900, "erp:provision")(w, f)
	})
	s, _, now := source(t, h)
	first, _ := s.Token(context.Background())
	*now = now.Add(10 * time.Minute) // inside the 80% window of a 15 minute token
	if again, _ := s.Token(context.Background()); again != first || h.calls.Load() != 1 {
		t.Fatalf("a token in date was exchanged again (calls %d)", h.calls.Load())
	}
	*now = now.Add(3 * time.Minute) // past 80%: replaced before it can expire
	second, err := s.Token(context.Background())
	if err != nil || second == first || h.calls.Load() != 2 {
		t.Fatalf("not refreshed ahead of expiry: %q %q calls %d err %v", first, second, h.calls.Load(), err)
	}
}

func TestAssertionIsReadAgainOnEveryExchangeSoRotationNeedsNoRestart(t *testing.T) {
	h := newHydra(t, ok("a", 60, "erp:provision"))
	s, file, now := source(t, h)
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	rotated := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJyb3RhdGVkIn0.cm90YXRlZA"
	if err := os.WriteFile(file, []byte(rotated), 0o600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.requests[1]["assertion"] != rotated {
		t.Fatal("the rotated assertion was not used")
	}
}

func TestLifetimeIsCappedAtTheGovernedFifteenMinutes(t *testing.T) {
	h := newHydra(t, ok("a", 86400, "erp:provision"))
	s, _, now := source(t, h)
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(14 * time.Minute)
	if _, err := s.Token(context.Background()); err != nil || h.calls.Load() != 2 {
		t.Fatalf("a day-long answer was cached beyond 15 minutes (calls %d, err %v)", h.calls.Load(), err)
	}
}

func TestRefusedAnswersAreNeverUsedAndNeverLeakAnything(t *testing.T) {
	secret := "SECRET-TOKEN-MATERIAL"
	for name, answer := range map[string]func(http.ResponseWriter, map[string]string){
		"error-status": func(w http.ResponseWriter, _ map[string]string) {
			http.Error(w, secret, http.StatusInternalServerError)
		},
		"oauth-error": func(w http.ResponseWriter, _ map[string]string) {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"` + secret + `"}`))
		},
		"not-json": func(w http.ResponseWriter, _ map[string]string) { _, _ = w.Write([]byte(secret)) },
		"no-token": ok("", 900, "erp:provision"),
		"mac-token-type": func(w http.ResponseWriter, _ map[string]string) {
			_, _ = w.Write([]byte(`{"access_token":"` + secret + `","token_type":"mac","expires_in":900}`))
		},
		"no-lifetime":      ok(secret, 0, "erp:provision"),
		"extra-scope":      ok(secret, 900, "erp:provision erp:read"),
		"other-scope":      ok(secret, 900, "erp:read"),
		"space-in-token":   ok("a b", 900, "erp:provision"),
		"oversized-answer": func(w http.ResponseWriter, _ map[string]string) { _, _ = w.Write([]byte(strings.Repeat("x", 70<<10))) },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHydra(t, answer)
			s, _, _ := source(t, h)
			token, err := s.Token(context.Background())
			if err == nil || token != "" {
				t.Fatalf("accepted: %q", token)
			}
			for _, leak := range []string{secret, assertion} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("error leaks material: %v", err)
				}
			}
		})
	}
}

func TestOAuthErrorCodeIsReportedButNotItsDescription(t *testing.T) {
	h := newHydra(t, func(w http.ResponseWriter, _ map[string]string) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"details"}`))
	})
	s, _, _ := source(t, h)
	_, err := s.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") || strings.Contains(err.Error(), "details") {
		t.Fatalf("got %v", err)
	}
}

func TestRedirectIsNeverFollowedSoTheAssertionCannotBeCarriedElsewhere(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	t.Cleanup(other.Close)
	h := newHydra(t, func(w http.ResponseWriter, _ map[string]string) {
		w.Header().Set("Location", other.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	s, _, _ := source(t, h)
	if _, err := s.Token(context.Background()); err == nil || elsewhere.Load() != 0 {
		t.Fatalf("redirect followed (hits elsewhere %d, err %v)", elsewhere.Load(), err)
	}
	// Even a caller-supplied client cannot re-enable redirects.
	s.HTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	if _, err := s.Token(context.Background()); err == nil || elsewhere.Load() != 0 {
		t.Fatal("a supplied client re-enabled redirects")
	}
}

func TestFailedRefreshKeepsServingATokenThatIsStillInDateButNotAnExpiredOne(t *testing.T) {
	healthy := atomic.Bool{}
	healthy.Store(true)
	h := newHydra(t, func(w http.ResponseWriter, f map[string]string) {
		if healthy.Load() {
			ok("access-1", 900, "erp:provision")(w, f)
			return
		}
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	s, _, now := source(t, h)
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	healthy.Store(false)
	*now = now.Add(13 * time.Minute) // refresh is due, the token is still valid
	if token, err := s.Token(context.Background()); err != nil || token != "access-1" {
		t.Fatalf("a still-valid token was dropped: %q %v", token, err)
	}
	*now = now.Add(2 * time.Minute) // now past expiry
	if token, err := s.Token(context.Background()); err == nil || token != "" {
		t.Fatalf("an expired token was served: %q", token)
	}
}

func TestConcurrentCallersShareOneExchange(t *testing.T) {
	h := newHydra(t, func(w http.ResponseWriter, f map[string]string) {
		time.Sleep(50 * time.Millisecond)
		ok("a", 900, "erp:provision")(w, f)
	})
	s, _, _ := source(t, h)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Token(context.Background()) }()
	}
	wg.Wait()
	if h.calls.Load() != 1 {
		t.Fatalf("%d exchanges for 20 concurrent callers", h.calls.Load())
	}
}

func TestBadAssertionFilesAreRefusedBeforeAnythingIsSent(t *testing.T) {
	h := newHydra(t, ok("a", 900, "erp:provision"))
	s, file, _ := source(t, h)
	for name, content := range map[string]string{"empty": "", "not-a-jwt": "plain-text", "spaces": "a.b.c d", "huge": strings.Repeat("a.", 10<<10) + "b"} {
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Token(context.Background()); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Token(context.Background()); err == nil {
		t.Error("a missing assertion file accepted")
	}
	if h.calls.Load() != 0 {
		t.Fatalf("%d requests were sent with a bad assertion", h.calls.Load())
	}
}

func TestValidateRefusesUnsafeOrIncompleteConfiguration(t *testing.T) {
	good := func() *JWTBearer {
		return &JWTBearer{TokenURL: "https://issuer.example/oauth2/token", ClientID: "c", AssertionFile: "/f", Scope: []string{"erp:provision"}}
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*JWTBearer){
		"plain-http":     func(s *JWTBearer) { s.TokenURL = "http://issuer.example/oauth2/token" },
		"credentials":    func(s *JWTBearer) { s.TokenURL = "https://u:p@issuer.example/oauth2/token" },
		"query":          func(s *JWTBearer) { s.TokenURL = "https://issuer.example/oauth2/token?x=1" },
		"relative":       func(s *JWTBearer) { s.TokenURL = "/oauth2/token" },
		"no-client":      func(s *JWTBearer) { s.ClientID = " " },
		"no-file":        func(s *JWTBearer) { s.AssertionFile = "" },
		"no-scope":       func(s *JWTBearer) { s.Scope = nil },
		"wildcard-scope": func(s *JWTBearer) { s.Scope = []string{"erp:*"} },
		"spaced-scope":   func(s *JWTBearer) { s.Scope = []string{"erp:provision erp:read"} },
	} {
		bad := good()
		mutate(bad)
		if bad.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
	local := good()
	local.TokenURL = "http://127.0.0.1:4444/oauth2/token"
	if err := local.Validate(); err != nil {
		t.Fatalf("loopback development refused: %v", err)
	}
}
