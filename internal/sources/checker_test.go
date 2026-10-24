package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newChecker() *Checker {
	return NewChecker(5 * time.Second)
}

func TestChecker_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>All about the thing</body></html>"))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/article")
	if result.Status != StatusOK {
		t.Fatalf("status = %q, want %q", result.Status, StatusOK)
	}
	if !result.Alive() {
		t.Fatalf("expected OK result to be Alive()")
	}
}

func TestChecker_Dead404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/gone")
	if result.Status != StatusDead {
		t.Fatalf("status = %q, want %q", result.Status, StatusDead)
	}
	if result.HTTPStatus != http.StatusNotFound {
		t.Fatalf("HTTPStatus = %d, want 404", result.HTTPStatus)
	}
}

func TestChecker_Dead410(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/removed")
	if result.Status != StatusDead {
		t.Fatalf("status = %q, want %q", result.Status, StatusDead)
	}
}

func TestChecker_BotProtected403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/blocked")
	if result.Status != StatusBotProtected {
		t.Fatalf("status = %q, want %q", result.Status, StatusBotProtected)
	}
	if !result.Alive() {
		t.Fatalf("bot-protected sources are still Alive() (policy allows them with an archive copy)")
	}
}

func TestChecker_ChallengePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<title>Just a moment...</title>"))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/challenge")
	if result.Status != StatusBotProtected {
		t.Fatalf("status = %q, want %q", result.Status, StatusBotProtected)
	}
}

func TestChecker_Soft404Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Sorry, the page could not be found.</body></html>"))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/soft")
	if result.Status != StatusSoft404 {
		t.Fatalf("status = %q, want %q", result.Status, StatusSoft404)
	}
}

func TestChecker_Soft404RedirectToHome(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/article", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("home page"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/article")
	if result.Status != StatusSoft404 {
		t.Fatalf("status = %q, want %q", result.Status, StatusSoft404)
	}
}

func TestChecker_Unreachable(t *testing.T) {
	result := newChecker().Check(context.Background(), "http://127.0.0.1:1/nope")
	if result.Status != StatusUnreachable {
		t.Fatalf("status = %q, want %q", result.Status, StatusUnreachable)
	}
	if result.Alive() {
		t.Fatalf("unreachable sources must not be Alive()")
	}
}
