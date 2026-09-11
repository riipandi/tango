package mailer

import (
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/riipandi/tango/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailerMailpitDelivery delivers a message through the real
// SMTP pipeline and verifies it via Mailpit's HTTP API. The relay
// is a throwaway testcontainers container (docker daemon required).
func TestMailerMailpitDelivery(t *testing.T) {
	ctx := t.Context()
	mp := testutils.StartMailpit(ctx, t)

	smtpHost, smtpPort, _ := strings.Cut(mp.SMTPAddr, ":")
	t.Setenv("MAILER_SMTP_HOST", smtpHost)
	t.Setenv("MAILER_SMTP_PORT", smtpPort)
	t.Setenv("MAILER_SMTP_USERNAME", mp.Username)
	t.Setenv("MAILER_SMTP_PASSWORD", mp.Password)
	cfg, err := config.Load(config.LoadOptions{})
	require.NoError(t, err)

	templates, err := fs.Sub(web.EmailTemplates, "email")
	require.NoError(t, err)

	ml := New(cfg.Mailer, Options{
		Templates: templates,
		Logger:    logger.NewMock(),
	})

	// Unique recipient per run: the search below can only match this run's message.
	recipient := fmt.Sprintf("it-%d@tango.test", time.Now().UnixNano())
	require.NoError(t, ml.Send(ctx, Message{
		To:       recipient,
		Subject:  "Tango mailer integration test",
		Template: "test-email",
	}))

	// Verify delivery through the Mailpit HTTP API.
	api := fetcher.New(fetcher.Options{BaseURL: mp.APIURL})
	defer api.Close()

	searchURL := mp.APIURL + "/api/v1/search?query=to:" + url.QueryEscape(recipient)

	var search struct {
		Total    int `json:"total"`
		Messages []struct {
			ID      string `json:"ID"`
			Subject string `json:"Subject"`
		} `json:"messages"`
	}
	require.Eventually(t, func() bool {
		if _, apiErr := api.GetJSON(ctx, searchURL, &search); apiErr != nil {
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
	_, err = api.GetJSON(ctx, mp.APIURL+"/api/v1/message/"+search.Messages[0].ID, &detail)
	require.NoError(t, err)

	assert.Contains(t, detail.HTML, config.AppName)
	assert.NotEmpty(t, detail.Text)
}
