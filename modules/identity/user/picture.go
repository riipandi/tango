package user

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"uuid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/storage"
)

// The bundled default picture answers for every account that has none. It is
// a frontend asset — `public/images/default-avatar.png`, shipped in
// the compiled SPA — and the read answers it by redirect, so the picture the
// account without one shows is the one the frontend already bundles. The
// path is relative, so the redirect resolves against whatever host the
// client reached: the API's own in production, the dev server through its
// proxy.
const DefaultPicturePath = "/images/default-avatar.png"

// The picture kinds the update accepts. The bytes decide, not a declared
// type: the kind is read off the magic bytes, so a renamed archive never
// lands in the picture slot.
var pictureKinds = []struct {
	magic []byte
	mime  string
}{
	{magic: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, mime: "image/png"},
	{magic: []byte{0xff, 0xd8, 0xff}, mime: "image/jpeg"},
	{magic: []byte("RIFF"), mime: "image/webp"}, // the WEBP form sits at offset 8
}

// The failures the picture procedures report. The handler maps them to
// connect codes, the way the account failures are mapped.
var (
	// ErrUnsupportedPicture is an update whose bytes name no accepted image
	// kind.
	ErrUnsupportedPicture = errors.New("user: the picture is not a PNG, JPEG, or WebP image")

	// ErrPicturesUnavailable is a picture procedure a run without the
	// storage engine cannot serve.
	ErrPicturesUnavailable = errors.New("user: picture storage is not available")
)

// Picture is one account's picture opened for reading. Default marks the
// bundled picture answering for an account that has none of its own: its
// body is empty, and the transport answers it with the asset the SPA ships.
type Picture struct {
	Body        io.ReadCloser
	ContentType string
	Default     bool
}

// pictureKey composes the storage key one account's picture lives under.
func pictureKey(userID uuid.UUID) string {
	key, err := storage.Key("avatars", userID.String(), "profile-picture")
	if err != nil {
		// A UUID and fixed segments cannot form an invalid key; the fallback
		// is here for the validator's contract, not for this composition.
		return "avatars/" + userID.String() + "/profile-picture"
	}
	return key
}

// sniffPictureType reads the picture's kind off its magic bytes. The WebP
// check reaches past the RIFF form to the format field, so a WAV audio file
// — the other RIFF resident — is refused.
func sniffPictureType(data []byte) (string, bool) {
	for _, kind := range pictureKinds {
		if !bytes.HasPrefix(data, kind.magic) {
			continue
		}
		if kind.mime == "image/webp" && (len(data) < 12 || string(data[8:12]) != "WEBP") {
			continue
		}
		return kind.mime, true
	}
	return "", false
}

// UpdateProfilePicture replaces an account's picture. The bytes are sniffed
// for their kind before anything is stored, then staged into the storage
// engine and synced in the request — a picture is small, and the read that
// follows the update must see it — so the replayed queue task the watcher
// also schedules finds nothing left to do. The account row names the key
// last: a crash before it leaves an orphan the garbage collection sweeps,
// never a picture the account cannot read.
func (s *Service) UpdateProfilePicture(ctx context.Context, id string, data []byte) error {
	if s.pictures == nil {
		return ErrPicturesUnavailable
	}
	userID, err := uuid.Parse(id)
	if err != nil {
		return ErrUserNotFound
	}
	// The row is read to establish the account exists: the guard decided who
	// may write this picture, and a key for an account that is gone would
	// leave a file nothing names.
	if _, err := s.repo.GetUser(ctx, s.pool, userID); err != nil {
		if errors.Is(err, datastore.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("user: read for picture update: %w", err)
	}
	mime, ok := sniffPictureType(data)
	if !ok {
		return ErrUnsupportedPicture
	}

	key := pictureKey(userID)
	metadata := map[string]any{"content_type": mime, "user_id": userID.String()}
	if err := s.pictures.Stage(ctx, key, bytes.NewReader(data), metadata); err != nil {
		return fmt.Errorf("user: stage picture: %w", err)
	}
	if err := s.pictures.Sync(ctx, key); err != nil {
		return fmt.Errorf("user: sync picture: %w", err)
	}
	if _, err := s.repo.SetAvatarURL(ctx, s.pool, userID, key); err != nil {
		return err
	}
	s.log.Info("user: profile picture updated",
		slog.String("user_id", userID.String()), slog.Int("bytes", len(data)))
	return nil
}

// ResetProfilePicture removes an account's picture: the stored file is
// deleted from the engine and the row's key cleared, so the account falls
// back to the bundled default. An account without a picture resets as a
// no-op — the answer the client asked for is already the state.
func (s *Service) ResetProfilePicture(ctx context.Context, id string) error {
	if s.pictures == nil {
		return ErrPicturesUnavailable
	}
	userID, err := uuid.Parse(id)
	if err != nil {
		return ErrUserNotFound
	}
	row, err := s.repo.GetUser(ctx, s.pool, userID)
	if errors.Is(err, datastore.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("user: read for picture reset: %w", err)
	}
	if row.AvatarURL != nil {
		if err := s.pictures.Delete(ctx, *row.AvatarURL); err != nil {
			return fmt.Errorf("user: delete picture: %w", err)
		}
	}
	if _, err := s.repo.SetAvatarURL(ctx, s.pool, userID, ""); err != nil {
		return err
	}
	s.log.Info("user: profile picture reset", slog.String("user_id", userID.String()))
	return nil
}

// ProfilePicture opens one account's picture for reading. An account without
// one — the column's nil — answers the bundled default, the same state a
// reset lands in. The content type travels from the manifest's metadata, the
// value the update recorded when it staged the bytes.
func (s *Service) ProfilePicture(ctx context.Context, id string) (Picture, error) {
	userID, err := uuid.Parse(id)
	if err != nil {
		return Picture{}, ErrUserNotFound
	}
	row, err := s.repo.GetUser(ctx, s.pool, userID)
	if errors.Is(err, datastore.ErrNoRows) {
		return Picture{}, ErrUserNotFound
	}
	if err != nil {
		return Picture{}, fmt.Errorf("user: read for picture: %w", err)
	}
	if row.AvatarURL == nil {
		return Picture{Body: io.NopCloser(bytes.NewReader(nil)), Default: true}, nil
	}

	manifest, err := s.pictures.Manifest(ctx, *row.AvatarURL)
	if errors.Is(err, storage.ErrNotFound) {
		// The row names a key the engine holds no file for: the picture was
		// lost without its row. The bundled default answers rather than an
		// error, because the state the client sees is the one a reset
		// produces.
		return Picture{Body: io.NopCloser(bytes.NewReader(nil)), Default: true}, nil
	}
	if err != nil {
		return Picture{}, fmt.Errorf("user: read picture manifest: %w", err)
	}
	mime, _ := manifest.Metadata["content_type"].(string)
	body, err := s.pictures.Open(ctx, *row.AvatarURL)
	if err != nil {
		return Picture{}, fmt.Errorf("user: open picture: %w", err)
	}
	return Picture{Body: body, ContentType: mime}, nil
}
