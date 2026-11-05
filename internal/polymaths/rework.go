// Package polymaths dispatches durable moderator feedback to the original task.
package polymaths

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

type Client struct {
	URL, APIKey string
	HTTP        *http.Client
}

// Feedback first reconciles the stable job marker. If the POST succeeded but
// its response was lost, a retry finds the previous feedback instead of posting
// it again. The remote API has no documented idempotency-key parameter.
func (c *Client) Feedback(ctx context.Context, job store.ReworkJob) (bool, error) {
	marker := "[8bitnews-rework:" + job.ID + "]"
	path := "/tasks/" + url.PathEscape(job.TaskID) + "/feedback"
	prior, status, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return status >= 400 && status < 500 && status != 429 && status != 408, err
	}
	if strings.Contains(string(prior), marker) {
		return false, nil
	}
	body := map[string]any{"text": marker + "\nЗамечание модератора к релизу " + job.ReleaseID + ":\n" + job.Note +
		"\nИсправь замечания в существующем сюжете, повтори необходимые проверки QA и Legal final и сдай новый release_receipt. Не выбирай новую новость.",
		"mode": "auto", "intent": "explanation", "from_agent": "8bitnews-hub"}
	_, status, err = c.request(ctx, http.MethodPost, path, body)
	return status >= 400 && status < 500 && status != 429 && status != 408, err
}

func (c *Client) request(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("PolyMaths transport failed")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("PolyMaths response read failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("PolyMaths HTTP %d", resp.StatusCode)
	}
	if !json.Valid(raw) {
		return nil, resp.StatusCode, fmt.Errorf("PolyMaths returned invalid JSON")
	}
	return raw, resp.StatusCode, nil
}

type ReworkStore interface {
	ClaimRework(context.Context) (store.ReworkJob, bool, error)
	FinishRework(context.Context, store.ReworkJob, string, bool) error
}

func Run(ctx context.Context, st ReworkStore, client *Client, log *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		job, ok, err := st.ClaimRework(ctx)
		if err != nil && ctx.Err() == nil {
			log.Error("rework claim failed", "error", err)
		}
		if err == nil && ok {
			workCtx, cancel := context.WithTimeout(ctx, time.Minute)
			permanent, dispatchErr := client.Feedback(workCtx, job)
			cancel()
			failure := ""
			if dispatchErr != nil {
				failure = dispatchErr.Error()
				log.Error("rework dispatch failed", "job_id", job.ID, "error", dispatchErr)
			}
			if err := st.FinishRework(ctx, job, failure, permanent); err != nil {
				log.Error("rework result persistence failed", "job_id", job.ID, "error", err)
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
