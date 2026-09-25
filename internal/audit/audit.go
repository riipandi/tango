// Package audit is the vocabulary an audit record is written in, and the
// recorder that writes it.
//
// It is infrastructure rather than a module: a record is written from the
// request that caused it, in the same transaction, so every feature that
// changes something reaches the recorder the way it reaches the pool — by
// invoking it from the container. The module that *reads* records is
// modules/auditlog; this package never serves a request.
//
// The split is the same one internal/queue keeps: the vocabulary and the
// writer are shared, the surface that exposes them belongs to a module.
package audit

// The event names a record carries. They are this application's own spelling,
// snake_case like every other field it writes, rather than upstream's
// SCREAMING_SNAKE: a client reads `sign_in` beside `status_code`, and the two
// vocabularies would be one more thing to translate.
//
// An event names what happened, not which procedure answered. `user_created`
// is written by both the administrator's CreateUser and the sign-up flow,
// because a reader asking "when was this account created" wants one answer.
//
// TODO(audit): sign-out has no event here because it has no procedure — the
// session feature is a scaffold, and an event for a path nothing can take
// would be a name no writer reaches.
const (
	// EventSignIn is a credential verified and a session opened.
	EventSignIn = "sign_in"

	// EventAccountCreated is an account that did not exist now does. Sign-up
	// and the administrator's own creation both write it.
	EventAccountCreated = "account_created"

	// EventAccountUpdated is an account's fields rewritten.
	EventAccountUpdated = "account_updated"

	// EventAccountDeleted is an account removed.
	EventAccountDeleted = "account_deleted"

	// EventEmailVerificationSent is a verification message submitted.
	EventEmailVerificationSent = "email_verification_sent"

	// EventEmailVerified is an address proven, which is the state change
	// rather than the message.
	EventEmailVerified = "email_verified"

	// EventProfilePictureUpdated is an account's picture replaced.
	EventProfilePictureUpdated = "profile_picture_updated"

	// EventProfilePictureReset is an account's picture cleared.
	EventProfilePictureReset = "profile_picture_reset"
)

// The trigger values the trigger_type column's enum allows. A record this
// application writes is always triggered by a caller or by the application
// itself; `external` is left for a record a third party causes, and nothing
// writes it today.
const (
	TriggerUser   = "user"
	TriggerSystem = "system"
)

// The action_status values the column's enum allows. A record is written when
// the action completed or when it was refused; `pending` and `unknown` are in
// the enum but nothing writes them, because a record is written once, after
// the outcome is known.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)
