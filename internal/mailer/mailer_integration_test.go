package mailer

import (
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mailpitAPI is the Mailpit HTTP API from compose.yaml (:8025).
const mailpitAPI = "http://localhost:8025"

// TestMailerMailpitDelivery is an integration test against the
// Mailpit relay from compose.yaml (SMTP :1025, API :8025). It skips
// itself when Mailpit is not reachable — start it with:
//
//	task compose:up   (or: docker compose up -d mailpit)
//
// then run: task test:integration
func TestMailerMailpitDelivery(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1025", 500*time.Millisecond)
	if err != nil {
		t.Skip("mailpit not reachable — start it with: docker compose up -d mailpit")
	}
	conn.Close()

	// The compose relay enforces auth (MP_SMTP_AUTH); supply the
	// first credential pair through the real config pipeline.
	t.Setenv("MAILER_SMTP_USERNAME", "maileruser1")
	t.Setenv("MAILER_SMTP_PASSWORD", "mailerpass1")
	cfg, err := config.Load(config.LoadOptions{})
	require.NoError(t, err)

	templates, err := fs.Sub(web.EmailTemplates, "email")
	require.NoError(t, err)

	ml := New(cfg.Mailer, Options{
		Templates: templates,
		Logger:    logger.NewMock(),
	})

	// Unique recipient per run: the search below can only match
	// this run's message.
	recipient := fmt.Sprintf("it-%d@tango.test", time.Now().UnixNano())
	require.NoError(t, ml.Send(t.Context(), Message{
		To:       recipient,
		Subject:  "Tango mailer integration test",
		Template: "test-email",
	}))

	// Verify delivery through the Mailpit HTTP API.
	api := fetcher.New(fetcher.Options{BaseURL: mailpitAPI})
	defer api.Close()

	searchURL := mailpitAPI + "/api/v1/search?query=to:" + url.QueryEscape(recipient)

	var search struct {
		Total    int `json:"total"`
		Messages []struct {
			ID      string `json:"ID"`
			Subject string `json:"Subject"`
		} `json:"messages"`
	}
	require.Eventually(t, func() bool {
		if _, apiErr := api.GetJSON(t.Context(), searchURL, &search); apiErr != nil {
			return false
		}
		return search.Total >= 1
	}, 10*time.Second, 250*time.Millisecond, "message must appear in mailpit")

	require.NotEmpty(t, search.Messages)
	assert.Equal(t, "Tango mailer integration test", search.Messages[0].Subject)

	var detail struct {
		Subject string `json:"Subject"`
		HTML    string `json:"HTML"`
		Text    string `json:"Text"`
	}
	_, err = api.GetJSON(t.Context(), mailpitAPI+"/api/v1/message/"+search.Messages[0].ID, &detail)
	require.NoError(t, err)

	assert.Contains(t, detail.HTML, config.AppName)
	assert.NotEmpty(t, detail.Text)
}
