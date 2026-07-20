package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderNotificationPerType(t *testing.T) {
	tests := []struct {
		name         string
		notification slackNotification
		wantContains []string
	}{
		{
			name: "needs input",
			notification: slackNotification{
				ID: "ntf_1", Type: "needs_input", Title: "Session needs input",
				Body: "demo-1 is waiting", PRURL: "",
			},
			wantContains: []string{"Session needs input", "demo-1 is waiting"},
		},
		{
			name: "ready to merge",
			notification: slackNotification{
				ID: "ntf_2", Type: "ready_to_merge", Title: "PR ready to merge",
				Body: "checks passed", PRURL: "https://github.com/o/r/pull/1",
			},
			wantContains: []string{"PR ready to merge", "checks passed", "https://github.com/o/r/pull/1"},
		},
		{
			name: "pr merged",
			notification: slackNotification{
				ID: "ntf_3", Type: "pr_merged", Title: "PR merged",
				Body: "shipped", PRURL: "https://github.com/o/r/pull/2",
			},
			wantContains: []string{"PR merged", "shipped", "https://github.com/o/r/pull/2"},
		},
		{
			name: "pr closed unmerged",
			notification: slackNotification{
				ID: "ntf_4", Type: "pr_closed_unmerged", Title: "PR closed",
				Body: "abandoned", PRURL: "https://github.com/o/r/pull/3",
			},
			wantContains: []string{"PR closed", "abandoned", "https://github.com/o/r/pull/3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderNotification(tc.notification)
			if strings.Contains(got, "\n") {
				t.Fatalf("rendered message must be a single line, got %q", got)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Fatalf("rendered %q does not contain %q", got, want)
				}
			}
		})
	}
}

func TestRenderNotificationWithoutPRURL(t *testing.T) {
	got := renderNotification(slackNotification{
		ID: "ntf_5", Type: "needs_input", Title: "Needs input", Body: "waiting",
	})
	if strings.Contains(got, "<http") || strings.Contains(got, "|View PR>") {
		t.Fatalf("expected no link when PR URL is empty, got %q", got)
	}
	if !strings.Contains(got, "Needs input") || !strings.Contains(got, "waiting") {
		t.Fatalf("expected title and body in %q", got)
	}
}

func TestRenderNotificationUnknownTypeFallsBackToPayload(t *testing.T) {
	got := renderNotification(slackNotification{
		ID: "ntf_6", Type: "some_future_type", Title: "Future thing", Body: "details here",
	})
	if !strings.Contains(got, "Future thing") || !strings.Contains(got, "details here") {
		t.Fatalf("unknown type must still render its own title and body, got %q", got)
	}
}

func TestRenderNotificationEscapesSlackControlCharacters(t *testing.T) {
	got := renderNotification(slackNotification{
		ID: "ntf_7", Type: "pr_merged", Title: "fix: a & b", Body: "<script> & </script>",
	})
	if strings.Contains(got, "<script>") {
		t.Fatalf("expected angle brackets in text to be escaped, got %q", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Fatalf("expected ampersand to be escaped, got %q", got)
	}
}

func TestRenderNotificationEmptyBody(t *testing.T) {
	got := renderNotification(slackNotification{
		ID: "ntf_8", Type: "pr_merged", Title: "PR merged",
	})
	if !strings.Contains(got, "PR merged") {
		t.Fatalf("expected title in %q", got)
	}
	if strings.HasSuffix(strings.TrimSpace(got), "—") {
		t.Fatalf("expected no dangling separator for an empty body, got %q", got)
	}
}

func TestSlackPosterSendsTextPayload(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	poster := slackPoster{client: srv.Client(), webhookURL: srv.URL}
	if err := poster.post(context.Background(), "hello world"); err != nil {
		t.Fatalf("post: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("content type = %q, want application/json", gotContentType)
	}
	var payload map[string]string
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body %q: %v", gotBody, err)
	}
	if payload["text"] != "hello world" {
		t.Errorf("text = %q, want %q", payload["text"], "hello world")
	}
}

func TestSlackPosterErrorsOnNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("invalid_token"))
	}))
	defer srv.Close()

	poster := slackPoster{client: srv.Client(), webhookURL: srv.URL}
	err := poster.post(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected an error for a non-2xx Slack response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected the status code in %q", err.Error())
	}
}
