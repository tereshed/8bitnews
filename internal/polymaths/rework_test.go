package polymaths

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tereshed/8bitnews/internal/store"
)

func TestFeedbackReconcilesLostResponse(t *testing.T) {
	var posted []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Error("missing authentication")
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"feedback": posted})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		posted = append(posted, body)
		// The remote operation commits, but its response is lost.
		h, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, _ := h.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, APIKey: "test-key", HTTP: srv.Client()}
	j := store.ReworkJob{ID: "job-one", TaskID: "task-one", ReleaseID: "release-one", Note: "Увеличить заголовок"}
	if _, err := c.Feedback(context.Background(), j); err == nil {
		t.Fatal("lost response must be retried")
	}
	if _, err := c.Feedback(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 {
		t.Fatalf("duplicate feedback: %d", len(posted))
	}
	if posted[0]["from_agent"] != "8bitnews-hub" || !strings.Contains(posted[0]["text"].(string), j.Note) {
		t.Fatalf("lost provenance: %+v", posted[0])
	}
}

func TestFeedbackDoesNotSendWhenReconciliationFails(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()
			c := &Client{URL: srv.URL, APIKey: "secret", HTTP: srv.Client()}
			permanent, err := c.Feedback(context.Background(), store.ReworkJob{TaskID: "task"})
			if err == nil || posts != 0 || permanent != (status == 401) {
				t.Fatalf("permanent=%v, posts=%d, error=%v", permanent, posts, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("secret in error")
			}
		})
	}
}
