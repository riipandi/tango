package user

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	commonv1 "github.com/riipandi/tango/gen/proto/go/tango/common/v1"
	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/gen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// GroupBinding is the neutral user↔group projection: the group
// fields the binding surface needs, without importing the usergroup
// package (which imports this one).
type GroupBinding struct {
	ID          string
	Name        string
	DisplayName string
	CreatedAt   string
	UpdatedAt   *string
}

// CredentialView is the neutral WebAuthn credential projection for
// the admin/self management surface.
type CredentialView struct {
	ID              string
	Name            string
	CredentialID    string // base64url of the raw credential id
	AttestationType string
	Transport       []string
	BackupEligible  bool
	BackupState     bool
	CreatedAt       string
	LastUsedAt      *string
}

// GroupBindingPort serves the user↔group bindings; implemented by
// the usergroup feature over its stores.
type GroupBindingPort interface {
	// GroupsForUser lists the groups the user belongs to.
	GroupsForUser(ctx context.Context, userID UserID) ([]GroupBinding, error)
	// ReplaceGroupsForUser atomically rebinds the user to exactly
	// the named group IDs.
	ReplaceGroupsForUser(ctx context.Context, userID UserID, groupIDs []string) error
}

// CredentialAdminPort serves the WebAuthn credential management
// surface; implemented by the passkey feature.
type CredentialAdminPort interface {
	// CredentialsForUser lists the user's registered credentials.
	CredentialsForUser(ctx context.Context, userID UserID) ([]CredentialView, error)
	// RenameCredential updates one credential's display name;
	// unknown IDs surface ErrNotFound.
	RenameCredential(ctx context.Context, userID UserID, credentialID, name string) (*CredentialView, error)
	// DeleteCredential removes one credential; unknown IDs surface
	// ErrNotFound.
	DeleteCredential(ctx context.Context, userID UserID, credentialID string) error
}

// BindRPCPorts wires the cross-feature ports the Connect surface
// needs; called by the composition root after every feature exists.
func (s *Service) BindRPCPorts(groups GroupBindingPort, credentials CredentialAdminPort) {
	s.groupPort = groups
	s.credentialPort = credentials
}

// RPCService returns the Connect registration for the user surface:
// the procedure prefix and the handler. Admin CRUD and the self
// profile mix in one service, so the guard is per procedure; the
// access authenticator comes from the composition root.
func (s *Service) RPCService(auth kernel.AccessAuthenticator) (string, http.Handler) {
	admin := map[string]bool{
		identityv1connect.UserServiceListUsersProcedure:                true,
		identityv1connect.UserServiceGetUserProcedure:                  true,
		identityv1connect.UserServiceCreateUserProcedure:               true,
		identityv1connect.UserServiceUpdateUserProcedure:               true,
		identityv1connect.UserServiceDeleteUserProcedure:               true,
		identityv1connect.UserServiceUpdateProfilePictureProcedure:     true,
		identityv1connect.UserServiceDeleteProfilePictureProcedure:     true,
		identityv1connect.UserServiceListUserGroupsProcedure:           true,
		identityv1connect.UserServiceReplaceUserGroupsProcedure:        true,
		identityv1connect.UserServiceListWebAuthnCredentialsProcedure:  true,
		identityv1connect.UserServiceUpdateWebAuthnCredentialProcedure: true,
		identityv1connect.UserServiceDeleteWebAuthnCredentialProcedure: true,
	}
	self := map[string]bool{
		identityv1connect.UserServiceUpdateMeProcedure:               true,
		identityv1connect.UserServiceUpdateMyProfilePictureProcedure: true,
		identityv1connect.UserServiceDeleteMyProfilePictureProcedure: true,
	}
	prefix, handler := identityv1connect.NewUserServiceHandler(&userRPC{service: s},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(auth, admin, self)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

type userRPC struct {
	service *Service
}

// principalID resolves the caller's own user ID from the guarded
// context.
func (h *userRPC) principalID(ctx context.Context) (UserID, error) {
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok {
		return UserID{}, rpcerr.Unauthenticated("authentication required")
	}
	id, err := identity.ParseID[UserID](principal.UserID)
	if err != nil {
		return UserID{}, rpcerr.Unauthenticated("authentication required")
	}
	return id, nil
}

// savePicture stores raw bytes as the user's profile picture; the
// blob write and column update mirror the REST multipart flow, with
// the extension derived from the detected content type.
func (s *Service) savePicture(ctx context.Context, id UserID, image []byte) (User, error) {
	if s.images == nil {
		return User{}, rpcerr.Unimplemented("profile pictures are not wired")
	}
	if len(image) > maxPictureUpload {
		return User{}, rpcerr.InvalidArgument("file too large")
	}
	ext := pictureExtFromBytes(image)
	if ext == "" {
		return User{}, rpcerr.InvalidArgument("unsupported_file_type")
	}

	picturePath := "profile-pictures/" + id.String() + ext
	if err := s.images.Save(ctx, picturePath, bytes.NewReader(image)); err != nil {
		return User{}, rpcerr.Internal("internal error")
	}
	if err := s.store.SetProfilePicturePath(ctx, id, &picturePath); err != nil {
		_ = s.images.Delete(ctx, picturePath)
		return User{}, rpcError(err)
	}
	u, err := s.store.GetByID(ctx, id)
	if err != nil {
		return User{}, rpcError(err)
	}
	return u, nil
}

// clearPicture drops the user's profile picture; a missing blob
// stays a success.
func (s *Service) clearPicture(ctx context.Context, id UserID) (User, error) {
	if s.images == nil {
		return User{}, rpcerr.Unimplemented("profile pictures are not wired")
	}
	u, err := s.store.GetByID(ctx, id)
	if err != nil {
		return User{}, rpcError(err)
	}
	if err := s.store.SetProfilePicturePath(ctx, id, nil); err != nil {
		return User{}, rpcError(err)
	}
	if u.ProfilePicturePath != nil {
		_ = s.images.Delete(ctx, *u.ProfilePicturePath)
	}
	return s.store.GetByID(ctx, id)
}

// pictureExtFromBytes sniffes the allowlisted image type from the
// first bytes.
func pictureExtFromBytes(image []byte) string {
	switch http.DetectContentType(image) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// targetID parses a path user ID; TypeID-only, unknown shapes 404.
func targetID(raw string) (UserID, error) {
	id, err := identity.ParseID[UserID](raw)
	if err != nil {
		return UserID{}, rpcerr.NotFound("user not found")
	}
	return id, nil
}

func (h *userRPC) ListUsers(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[identityv1.ListUsersResponse], error) {
	page, limit := int(req.Msg.GetPage()), int(req.Msg.GetLimit())
	users, total, err := h.service.List(ctx, ListParams{
		Query: req.Msg.GetQuery(),
		Page:  Page{Page: page, Limit: limit},
	})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*identityv1.User, 0, len(users))
	for _, u := range users {
		out = append(out, ProtoView(u))
	}
	var metadata *commonv1.PageMetadata
	if page >= 1 && limit >= 1 {
		metadata = rpcerr.PageMetadata(page, limit, total)
	}
	return connect.NewResponse(&identityv1.ListUsersResponse{Users: out, Metadata: metadata}), nil
}

func (h *userRPC) GetUser(ctx context.Context, req *connect.Request[identityv1.GetUserRequest]) (*connect.Response[identityv1.User], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	u, err := h.service.GetByID(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

// derefText maps a proto optional string onto the plain string the
// create params use (nil → empty, matching the REST payload).
func derefText(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (h *userRPC) CreateUser(ctx context.Context, req *connect.Request[identityv1.CreateUserRequest]) (*connect.Response[identityv1.User], error) {
	params := CreateParams{
		Username:    req.Msg.GetUsername(),
		Email:       req.Msg.GetEmail(),
		FirstName:   derefText(req.Msg.FirstName),
		LastName:    derefText(req.Msg.LastName),
		DisplayName: derefText(req.Msg.DisplayName),
		IsAdmin:     req.Msg.GetIsAdmin(),
	}
	// Validate normalizes the fields and derives the display name.
	if _, verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	u, err := h.service.Create(ctx, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

func (h *userRPC) UpdateUser(ctx context.Context, req *connect.Request[identityv1.UpdateUserRequest]) (*connect.Response[identityv1.User], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	params := AdminUpdateParams{
		Email:       optionalText(req.Msg.Email),
		FirstName:   optionalText(req.Msg.FirstName),
		LastName:    optionalText(req.Msg.LastName),
		DisplayName: optionalText(req.Msg.DisplayName),
		IsAdmin:     optionalBool(req.Msg.IsAdmin),
		Disabled:    optionalBool(req.Msg.Disabled),
	}
	if verr := validateAdminUpdate(params); verr != nil {
		return nil, validationError(verr)
	}
	u, err := h.service.Update(ctx, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

func (h *userRPC) DeleteUser(ctx context.Context, req *connect.Request[identityv1.DeleteUserRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if err := h.service.Delete(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *userRPC) UpdateMe(ctx context.Context, req *connect.Request[identityv1.UpdateProfileRequest]) (*connect.Response[identityv1.User], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	params := UpdateProfileParams{
		FirstName:   optionalText(req.Msg.FirstName),
		LastName:    optionalText(req.Msg.LastName),
		DisplayName: optionalText(req.Msg.DisplayName),
		AvatarURL:   optionalText(req.Msg.AvatarUrl),
		Locale:      optionalText(req.Msg.Locale),
	}
	u, err := h.service.store.UpdateProfile(ctx, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

// UpdateMyProfilePicture stores the caller's own picture from raw
// bytes; validation (type, size) lives in the picture helper.
func (h *userRPC) UpdateMyProfilePicture(ctx context.Context, req *connect.Request[identityv1.UpdateMyProfilePictureRequest]) (*connect.Response[identityv1.User], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.service.savePicture(ctx, id, req.Msg.GetImage())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

func (h *userRPC) DeleteMyProfilePicture(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.User], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.service.clearPicture(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

func (h *userRPC) UpdateProfilePicture(ctx context.Context, req *connect.Request[identityv1.UpdateProfilePictureRequest]) (*connect.Response[identityv1.User], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	u, err := h.service.savePicture(ctx, id, req.Msg.GetImage())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

func (h *userRPC) DeleteProfilePicture(ctx context.Context, req *connect.Request[identityv1.DeleteProfilePictureRequest]) (*connect.Response[identityv1.User], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	u, err := h.service.clearPicture(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(ProtoView(u)), nil
}

func (h *userRPC) ListUserGroups(ctx context.Context, req *connect.Request[identityv1.ListUserGroupsRequest]) (*connect.Response[identityv1.ListUserGroupsResponse], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if h.service.groupPort == nil {
		return nil, rpcerr.Unimplemented("user groups are not wired")
	}
	bindings, err := h.service.groupPort.GroupsForUser(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*identityv1.UserGroup, 0, len(bindings))
	for _, g := range bindings {
		view := &identityv1.UserGroup{
			Id:          g.ID,
			Name:        g.Name,
			DisplayName: g.DisplayName,
			CreatedAt:   g.CreatedAt,
		}
		if g.UpdatedAt != nil {
			view.UpdatedAt = g.UpdatedAt
		}
		out = append(out, view)
	}
	return connect.NewResponse(&identityv1.ListUserGroupsResponse{Groups: out}), nil
}

func (h *userRPC) ReplaceUserGroups(ctx context.Context, req *connect.Request[identityv1.ReplaceUserGroupsRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if h.service.groupPort == nil {
		return nil, rpcerr.Unimplemented("user groups are not wired")
	}
	if err := h.service.groupPort.ReplaceGroupsForUser(ctx, id, req.Msg.GetGroupIds()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *userRPC) ListWebAuthnCredentials(ctx context.Context, req *connect.Request[identityv1.ListWebAuthnCredentialsRequest]) (*connect.Response[identityv1.ListWebAuthnCredentialsResponse], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if h.service.credentialPort == nil {
		return nil, rpcerr.Unimplemented("webauthn credentials are not wired")
	}
	credentials, err := h.service.credentialPort.CredentialsForUser(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*identityv1.WebAuthnCredential, 0, len(credentials))
	for _, c := range credentials {
		out = append(out, credentialView(c))
	}
	return connect.NewResponse(&identityv1.ListWebAuthnCredentialsResponse{Credentials: out}), nil
}

func (h *userRPC) UpdateWebAuthnCredential(ctx context.Context, req *connect.Request[identityv1.UpdateWebAuthnCredentialRequest]) (*connect.Response[identityv1.WebAuthnCredential], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if h.service.credentialPort == nil {
		return nil, rpcerr.Unimplemented("webauthn credentials are not wired")
	}
	credential, err := h.service.credentialPort.RenameCredential(ctx, id, req.Msg.GetCredentialId(), req.Msg.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(credentialView(*credential)), nil
}

func (h *userRPC) DeleteWebAuthnCredential(ctx context.Context, req *connect.Request[identityv1.DeleteWebAuthnCredentialRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if h.service.credentialPort == nil {
		return nil, rpcerr.Unimplemented("webauthn credentials are not wired")
	}
	if err := h.service.credentialPort.DeleteCredential(ctx, id, req.Msg.GetCredentialId()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// credentialView maps the neutral projection onto the wire message.
func credentialView(c CredentialView) *identityv1.WebAuthnCredential {
	out := &identityv1.WebAuthnCredential{
		Id:              c.ID,
		Name:            &c.Name,
		CredentialId:    c.CredentialID,
		AttestationType: &c.AttestationType,
		Transport:       c.Transport,
		BackupEligible:  &c.BackupEligible,
		BackupState:     &c.BackupState,
		CreatedAt:       &c.CreatedAt,
	}
	if c.LastUsedAt != nil {
		out.LastUsedAt = c.LastUsedAt
	}
	return out
}

// rpcError maps user domain sentinels onto Connect codes; connect
// errors raised inside the service pass through unchanged.
func rpcError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("user not found")
	case errors.Is(err, ErrDuplicate):
		return rpcerr.AlreadyExists(err.Error())
	case errors.Is(err, ErrInvalidUsername), errors.Is(err, ErrInvalidEmail):
		return rpcerr.InvalidArgument(err.Error())
	default:
		return rpcerr.Internal("internal error")
	}
}

// validationError maps ozzo field errors onto invalid_argument.
func validationError(verr error) error {
	parts := make([]string, 0, 4)
	for _, fe := range validate.FieldErrors(verr) {
		parts = append(parts, fmt.Sprintf("%s: %s", fe.Field, fe.Message))
	}
	return rpcerr.InvalidArgument("validation failed: " + strings.Join(parts, "; "))
}

// validateAdminUpdate enforces the admin payload rules: a provided
// email must parse, a provided display name must be non-empty.
func validateAdminUpdate(params AdminUpdateParams) error {
	if params.Email != nil {
		if err := validation.Validate(*params.Email, is.Email); err != nil {
			return validation.Errors{"email": err}
		}
	}
	if params.DisplayName != nil && *params.DisplayName == "" {
		return validation.Errors{"display_name": errors.New("must not be empty")}
	}
	return nil
}

// optionalText maps a proto optional string onto the *string the
// domain params use — pointer identity is preserved (nil = keep).
func optionalText(v *string) *string {
	return v
}

// optionalBool maps a proto optional bool onto the *bool the domain
// params use — pointer identity is preserved (nil = keep).
func optionalBool(v *bool) *bool {
	return v
}
