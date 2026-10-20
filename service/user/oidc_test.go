package user

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Mxucc/StorFlow/pkg/serializer"
)

func TestSanitizeOIDCRedirect(t *testing.T) {
	cases := map[string]string{
		"":                    "/home",
		"//evil.example.com":  "/home",
		"http://evil.example": "/home",
		"/\\evil.example":     "/home",
		"home":                "/home",
		"/":                   "/",
		"/home":               "/home",
		"/s/abcd?x=1":         "/s/abcd?x=1",
	}

	for input, expected := range cases {
		if got := SanitizeOIDCRedirect(input); got != expected {
			t.Errorf("SanitizeOIDCRedirect(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestOIDCRandomToken(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 16; i++ {
		token, err := oidcRandomToken()
		if err != nil {
			t.Fatalf("oidcRandomToken() returned error: %s", err)
		}
		if len(token) != 64 {
			t.Fatalf("oidcRandomToken() length = %d, want 64", len(token))
		}
		if _, err := hex.DecodeString(token); err != nil {
			t.Fatalf("oidcRandomToken() = %q is not hex encoded: %s", token, err)
		}
		if _, ok := seen[token]; ok {
			t.Fatalf("oidcRandomToken() returned duplicated value %q", token)
		}
		seen[token] = struct{}{}
	}
}

func TestOIDCClaimHelpers(t *testing.T) {
	claims := map[string]any{
		"sub":            "user-1",
		"email":          "someone@example.com",
		"email_verified": true,
		"numeric":        json.Number("42"),
		"string_bool":    "true",
	}

	if got := oidcClaimString(claims, "sub"); got != "user-1" {
		t.Errorf("oidcClaimString(sub) = %q, want %q", got, "user-1")
	}
	if got := oidcClaimString(claims, "numeric"); got != "42" {
		t.Errorf("oidcClaimString(numeric) = %q, want %q", got, "42")
	}
	if got := oidcClaimString(claims, "missing"); got != "" {
		t.Errorf("oidcClaimString(missing) = %q, want empty", got)
	}
	if got := oidcClaimString(claims, ""); got != "" {
		t.Errorf("oidcClaimString(empty name) = %q, want empty", got)
	}
	if !oidcClaimBool(claims, "email_verified") {
		t.Error("oidcClaimBool(email_verified) = false, want true")
	}
	if !oidcClaimBool(claims, "string_bool") {
		t.Error("oidcClaimBool(string_bool) = false, want true")
	}
	if oidcClaimBool(claims, "sub") {
		t.Error("oidcClaimBool(sub) = true, want false")
	}
}

func TestOIDCCallbackErrorLoginLocation(t *testing.T) {
	err := NewOIDCCallbackError(serializer.CodeCredentialInvalid, "boom & bust")
	if err.Code != serializer.CodeCredentialInvalid {
		t.Fatalf("Code = %d, want %d", err.Code, serializer.CodeCredentialInvalid)
	}

	location := err.LoginLocation()
	if !strings.HasPrefix(location, "/login?sso_error=40020&sso_msg=") {
		t.Fatalf("LoginLocation() = %q, want /login?sso_error=40020&sso_msg=...", location)
	}

	parsed, parseErr := url.Parse(location)
	if parseErr != nil {
		t.Fatalf("LoginLocation() is not a valid URL: %s", parseErr)
	}
	if got := parsed.Query().Get("sso_msg"); got != "boom & bust" {
		t.Fatalf("sso_msg = %q, want %q", got, "boom & bust")
	}
}

// TestDiscoverOIDCProviderCaches verifies the discovery document is fetched only
// once per issuer and that switching the issuer invalidates the cache.
func TestDiscoverOIDCProviderCaches(t *testing.T) {
	ResetOIDCProviderCache()
	t.Cleanup(ResetOIDCProviderCache)

	var hits int64
	newIdP := func() *httptest.Server {
		mux := http.NewServeMux()
		var server *httptest.Server
		mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&hits, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"issuer": %q,
				"authorization_endpoint": %q,
				"token_endpoint": %q,
				"jwks_uri": %q,
				"response_types_supported": ["code"],
				"subject_types_supported": ["public"],
				"id_token_signing_alg_values_supported": ["RS256"]
			}`, server.URL, server.URL+"/authorize", server.URL+"/token", server.URL+"/jwks")
		})
		server = httptest.NewServer(mux)

		return server
	}

	idp := newIdP()
	defer idp.Close()

	if _, err := discoverOIDCProvider(t.Context(), idp.URL); err != nil {
		t.Fatalf("first discovery failed: %s", err)
	}
	if _, err := discoverOIDCProvider(t.Context(), idp.URL); err != nil {
		t.Fatalf("second discovery failed: %s", err)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("discovery document was fetched %d times, want 1", got)
	}

	otherIdP := newIdP()
	defer otherIdP.Close()

	if _, err := discoverOIDCProvider(t.Context(), otherIdP.URL); err != nil {
		t.Fatalf("discovery for the new issuer failed: %s", err)
	}
	// The old issuer is dropped when the configured endpoint changes, so it is
	// discovered again on the next request.
	if _, err := discoverOIDCProvider(t.Context(), idp.URL); err != nil {
		t.Fatalf("re-discovery of the old issuer failed: %s", err)
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Fatalf("discovery document was fetched %d times, want 3", got)
	}
}
