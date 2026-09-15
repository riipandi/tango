package jobs

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/riipandi/tango/web"
)

// TestEmailTaskSurvivesQueueRoundTripAndRenders checks queue decoding,
// template rendering, and SMTP delivery.
func TestEmailTaskSurvivesQueueRoundTripAndRenders(t *testing.T) {
	ctx := t.Context()
	mp := testutils.StartMailpit(ctx, t)

	db := testDB(t)
	queue := testQueue(t, db)
	registry := NewRegistry(queue, testMailer(t, mp), logger.NewMock())

	// Unique recipient per run: the search below can only match it.
	recipient := fmt.Sprintf("jobs-%d@tango.test", time.Now().UnixNano())

	expiry := now().Add(24 * time.Hour).Format("2006-01-02 15:04:05 MST")
	require.NoError(t, registry.EnqueueEmail(ctx, mailer.Message{
		To:       recipient,
		Subject:  "Your API key expires soon",
		Template: "api-key-expiring-soon",
		Data: map[string]any{
			"Name":       "Hook User",
			"APIKeyName": "test-key",
			"ExpiresAt":  expiry,
		},
	}))

	queue.Start(ctx)
	t.Cleanup(func() { queue.Stop(context.Background()) })

	// The rendered body must contain the pre-formatted date.
	var found bool
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !found {
		found = mailpitHas(t, mp, recipient, expiry)
		if !found {
			time.Sleep(500 * time.Millisecond)
		}
	}
	require.True(t, found, "the queued email must arrive rendered, with the formatted expiry")
}

// mailpitHas reports whether Mailpit has a matching message.
func mailpitHas(t *testing.T, mp *testutils.Mailpit, recipient, bodyFragment string) bool {
	t.Helper()

	api := fetcher.New(fetcher.Options{BaseURL: mp.APIURL, Logger: logger.NewMock()})
	defer api.Close()

	search := mp.APIURL + "/api/v1/search?query=to:" + url.QueryEscape(recipient)
	var list struct {
		Total    int `json:"total"`
		Messages []struct {
			ID string `json:"ID"`
		} `json:"messages"`
	}
	if _, err := api.GetJSON(t.Context(), search, &list); err != nil {
		return false
	}
	if list.Total == 0 {
		return false
	}

	var detail struct {
		Text string `json:"Text"`
	}
	if _, err := api.GetJSON(t.Context(),
		fmt.Sprintf("%s/api/v1/message/%s", mp.APIURL, list.Messages[0].ID), &detail); err != nil {
		return false
	}
	return strings.Contains(detail.Text, bodyFragment)
}

// testMailer builds a mailer over the embedded templates and Mailpit.
func testMailer(t *testing.T, mp *testutils.Mailpit) Mailer {
	t.Helper()

	smtpHost, smtpPort, _ := strings.Cut(mp.SMTPAddr, ":")
	t.Setenv("MAILER_SMTP_HOST", smtpHost)
	t.Setenv("MAILER_SMTP_PORT", smtpPort)
	t.Setenv("MAILER_SMTP_USERNAME", mp.Username)
	t.Setenv("MAILER_SMTP_PASSWORD", mp.Password)

	cfg, err := config.Load(config.LoadOptions{})
	require.NoError(t, err)

	templates, err := fs.Sub(web.EmailTemplates, "email")
	require.NoError(t, err)

	return mailer.New(cfg.Mailer, mailer.Options{
		Templates: templates,
		Logger:    logger.NewMock(),
	})
}

// TestEmailTaskDataLosesTimeTypes checks JSON conversion of time values.
func TestEmailTaskDataLosesTimeTypes(t *testing.T) {
	task := EmailTask{Data: map[string]any{
		"ExpiresAt": time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
	}}

	encoded, err := jsonv2.Marshal(task)
	require.NoError(t, err)

	var decoded EmailTask
	require.NoError(t, jsonv2.Unmarshal(encoded, &decoded))

	_, isTime := decoded.Data["ExpiresAt"].(time.Time)
	assert.False(t, isTime, "time.Time must not survive the queue round trip")
	assert.Equal(t, "2026-09-21T10:00:00Z", decoded.Data["ExpiresAt"])
}
