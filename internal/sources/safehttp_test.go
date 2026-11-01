package sources

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Some bot walls never answer a bot's HTTP/1.1 request but reset its
// HTTP/2 one at once (CBC, 2026-11-01): the checks must speak HTTP/2 where
// the server offers it, even though the SSRF dialer is a custom one.
func TestSafeHTTPClient_SpeaksHTTP2(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client := newSafeHTTPClient(5*time.Second, true)
	// trust the test server's certificate, as the system pool trusts a real site's
	client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ProtoMajor != 2 {
		t.Fatalf("Proto = %s, want HTTP/2", resp.Proto)
	}
}
