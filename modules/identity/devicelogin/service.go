package devicelogin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
)

// Service orchestrates the device login state machine. The
// approving device must present a live session.
type Service struct {
	store        Store
	sessions     *session.Service
	users        user.Store
	recorder     identity.Recorder
	cookieName   string
	cookieSecure bool
}

// NewService builds the device login feature.
func NewService(store Store, sessions *session.Service, users user.Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, sessions: sessions, users: users, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ServiceOption configures the feature.
type ServiceOption func(*Service)

// WithCookie wires the session cookie name + Secure flag for the
// exchange response.
func WithCookie(name string, secure bool) ServiceOption {
	return func(s *Service) { s.cookieName, s.cookieSecure = name, secure }
}

// Name names the feature for logs.
func (s *Service) Name() string { return "devicelogin" }

// Created carries the create response: the request id (the poll
// key) plus the QR payload.
type Created struct {
	ID                      string    `json:"id"`
	UserCode                string    `json:"user_code"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
}

// Create parks a new pending request for an anonymous device and
// returns its handle + device token secret.
func (s *Service) Create(ctx context.Context, appURL, ipAddress, userAgent string) (*Created, string, error) {
	deviceToken, err := newDeviceToken()
	if err != nil {
		return nil, "", err
	}

	for range 3 { // rare live-code collision retry
		code, codeErr := userCode()
		if codeErr != nil {
			return nil, "", codeErr
		}

		request := Request{
			Code:            code,
			DeviceTokenHash: hashToken(deviceToken),
			Status:          StatusPending,
			IPAddress:       ipAddress,
			UserAgent:       userAgent,
			ExpiresAt:       time.Now().UTC().Add(RequestDuration),
		}
		if insertErr := s.store.Insert(ctx, &request); insertErr != nil {
			if errors.Is(insertErr, ErrPending) {
				continue // code collision
			}
			return nil, "", insertErr
		}

		base := trimSlash(appURL)
		return &Created{
			ID:                      request.ID,
			UserCode:                request.Code,
			ExpiresAt:               request.ExpiresAt,
			Interval:                PollingInterval,
			VerificationURI:         base + "/login/device",
			VerificationURIComplete: base + "/login/device?code=" + request.Code,
		}, deviceToken, nil
	}
	return nil, "", errors.New("devicelogin: failed to allocate a unique code")
}

// Inspect returns what the approving device will see (device
// metadata + expiry) for a user code.
func (s *Service) Inspect(ctx context.Context, code string) (*VerificationInfo, error) {
	request, err := s.store.GetByCode(ctx, normalizeCode(code))
	if err != nil {
		return nil, err
	}
	if request.Status != StatusPending || time.Now().UTC().After(request.ExpiresAt) {
		return nil, ErrInvalidRequest
	}

	return &VerificationInfo{
		UserCode:  request.Code,
		Device:    deviceString(request.UserAgent),
		IPAddress: request.IPAddress,
		ExpiresAt: request.ExpiresAt,
	}, nil
}

// Decide approves or denies a pending request for the signed-in
// user.
func (s *Service) Decide(ctx context.Context, code, decision string, principal middleware.Principal) error {
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approve" && decision != "deny" {
		return ErrInvalidRequest
	}
	approved := decision == "approve"

	if approvedErr := s.store.Decide(ctx, normalizeCode(code), decisionStatus(approved), principal.UserID); approvedErr != nil {
		return approvedErr
	}

	action := "device_login.denied"
	if approved {
		action = "device_login.approved"
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: action, Actor: principal.UserID, Target: normalizeCode(code)}, nil)
	}
	return nil
}

// Exchange long-polls the request until approved, denied, or timed
// out; on approval the device gets a session token (cookie handled
// by the caller).
func (s *Service) Exchange(ctx context.Context, requestID, deviceToken string) (user.User, string, error) {
	deadline := time.Now().UTC().Add(LongPollDuration)
	tokenHash := hashToken(deviceToken)

	for {
		request, err := s.store.GetByID(ctx, requestID)
		if err != nil {
			return user.User{}, "", err
		}
		if request.DeviceTokenHash != tokenHash || time.Now().UTC().After(request.ExpiresAt) {
			return user.User{}, "", ErrInvalidRequest
		}

		switch request.Status {
		case StatusApproved:
			userID := ""
			if request.UserID != nil {
				userID = *request.UserID
			}
			uid, parseErr := identity.ParseID[user.UserID](userID)
			if parseErr != nil {
				return user.User{}, "", ErrInvalidRequest
			}

			// Consume first so concurrent exchanges can't double-
			// mint; lookup failures leave the request untouched.
			if consumeErr := s.store.Consume(ctx, request.ID); consumeErr != nil {
				return user.User{}, "", consumeErr
			}
			token, _, issueErr := s.sessions.IssueForUser(ctx, uid, "devicelogin", session.Meta{})
			if issueErr != nil {
				return user.User{}, "", issueErr
			}
			if s.recorder != nil {
				s.recorder.Record(ctx, identity.AuditEvent{Action: "user.remote_signed_in", Actor: uid.String(), Target: request.Code}, nil)
			}

			u, userErr := s.users.GetByID(ctx, uid)
			if userErr != nil {
				return user.User{}, "", userErr
			}
			return u, token, nil
		case StatusDenied:
			return user.User{}, "", ErrDenied
		case StatusPending:
			// keep polling
		default:
			// consumed / unknown — never resolves.
			return user.User{}, "", ErrInvalidRequest
		}

		if time.Now().UTC().After(deadline) {
			return user.User{}, "", ErrPending
		}
		select {
		case <-time.After(PollTick):
		case <-ctx.Done():
			return user.User{}, "", ctx.Err()
		}
	}
}

// decisionStatus maps the decision verb to the row status.
func decisionStatus(approved bool) string {
	if approved {
		return StatusApproved
	}
	return StatusDenied
}

// deviceString renders a compact device label from the user agent.
func deviceString(userAgent string) string {
	if userAgent == "" {
		return "Unknown device"
	}
	if len(userAgent) > 64 {
		return userAgent[:64]
	}
	return userAgent
}

// trimSlash strips a trailing slash.
func trimSlash(value string) string {
	if len(value) > 0 && value[len(value)-1] == '/' {
		return value[:len(value)-1]
	}
	return value
}
