package guard

import (
	"net/http"
	"strings"

	authv1 "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1"
	authv1connect "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1/authv1connect"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	systemv1connect "github.com/riipandi/tango/codegen/proto/go/tango/system/v1/systemv1connect"
	"github.com/riipandi/tango/pkg/jwtutils"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Entry declares the rule one procedure gets, and — for a self rule — the
// request field the account is named in.
//
// Prototype is the request message the field is resolved against. It is kept
// beside the field so the table can be checked against the contract: a field
// that no longer exists fails a test instead of becoming a guard that refuses
// everything.
type Entry struct {
	// Rule decides who may call the procedure.
	Rule Rule
	// Field is the request field a self rule compares against the caller. It
	// is empty for every other rule.
	Field string
	// Prototype is the request message Field belongs to.
	Prototype proto.Message
}

// ProcedureRules is the rule every procedure on the RPC surface gets.
//
// A procedure absent from this map is administrative. That is the default
// because forgetting to declare a new procedure must fail closed: an
// undeclared procedure refuses a caller who is not an administrator, which is
// a bug report, while an undeclared procedure answered openly is a breach
// nobody notices.
//
// The map is keyed by the generated procedure path, so a renamed contract
// breaks the build rather than leaving a rule attached to nothing.
var ProcedureRules = map[string]Entry{
	// The surfaces a caller reaches before they hold a token, or that a
	// monitor probes without one.
	systemv1connect.HealthServiceCheckProcedure:                    {Rule: Public},
	authv1connect.AuthServiceSignInProcedure:                       {Rule: Public},
	identityv1connect.SignupServiceSignupProcedure:                 {Rule: Public},
	identityv1connect.EmailVerificationServiceVerifyEmailProcedure: {Rule: Public},

	// The caller's own door: the procedure reads the account from the claims
	// and takes no target from the request, so being authenticated is the
	// whole requirement. Upstream answers this with a separate `/users/me`
	// route; here the account is the claims' subject, which is the same rule
	// without a second route.
	identityv1connect.EmailVerificationServiceSendEmailProcedure: {Rule: Authenticated},

	// Self-service with a target in the request: the account named must be
	// the caller's own.
	identityv1connect.UserServiceResetProfilePictureProcedure: {
		Rule:      Self("id"),
		Field:     "id",
		Prototype: &identityv1.ResetProfilePictureRequest{},
	},

	// Administrative, declared explicitly rather than left to the default so
	// the table reads as the complete policy of the surface. Upstream guards
	// every one of these with its admin-required middleware.
	identityv1connect.SignupServiceCreateSignupTokenProcedure: {Rule: Admin},
	identityv1connect.SignupServiceListSignupTokensProcedure:  {Rule: Admin},
	identityv1connect.SignupServiceDeleteSignupTokenProcedure: {Rule: Admin},
	identityv1connect.UserServiceListUsersProcedure:           {Rule: Admin},
	identityv1connect.UserServiceGetUserProcedure:             {Rule: Admin},
	identityv1connect.UserServiceCreateUserProcedure:          {Rule: Admin},
	identityv1connect.UserServiceUpdateUserProcedure:          {Rule: Admin},
	identityv1connect.UserServiceDeleteUserProcedure:          {Rule: Admin},
}

// RuleFor answers the rule a procedure gets. A procedure the table does not
// name is administrative, so a new procedure is protected the moment it is
// mounted.
func RuleFor(procedure string) Rule {
	if entry, ok := ProcedureRules[procedure]; ok && entry.Rule != nil {
		return entry.Rule
	}
	return Admin
}

// RestEntry declares the rule one REST route gets.
type RestEntry struct {
	// Method is the HTTP method the route answers.
	Method string
	// Pattern is the chi route pattern, with `{param}` for one segment.
	Pattern string
	// Rule decides who may call the route.
	Rule Rule
	// Param is the path parameter a self rule compares against the caller.
	Param string
}

// RestRules is the rule every REST route a module mounts gets.
//
// Like the procedure table, a route absent from it is administrative. The
// routes the application mounts itself — the API root, the readiness probe —
// are outside the guarded group and never reach this table.
var RestRules = []RestEntry{
	// The protocol surfaces a client reaches without a token. The key set is
	// public because a verifier needs it before it can hold a token; the
	// picture read is public because an <img> tag fetches it, and an account
	// without one answers the bundled default by redirect.
	{Method: http.MethodGet, Pattern: "/.well-known/jwks.json", Rule: Public},
	{Method: http.MethodGet, Pattern: "/api/users/{id}/profile-picture.png", Rule: Public},

	// The write is the account's own: the caller must be the account named in
	// the path. An administrator does not pass by virtue of the role — the
	// administrative procedures are the RPC surface's — which is the rule
	// upstream applies by offering `/users/me/profile-picture` separately.
	{
		Method:  http.MethodPut,
		Pattern: "/api/users/{id}/profile-picture",
		Rule:    Self("id"),
		Param:   "id",
	},
}

// RestRuleFor answers the rule a REST request gets, with the target it names.
//
// The route is matched segment by segment, so a `{param}` stands for exactly
// one segment and a path parameter is read from the position the pattern puts
// it in. Matching here rather than reading chi's route context is deliberate:
// the middleware runs before the route is matched, and a guard that waited
// for the match would run after the handler was chosen.
//
// A route the table does not name is administrative, the same default the
// procedure table applies.
func RestRuleFor(method, path string) (Rule, Target) {
	segments := splitPath(path)
	for _, entry := range RestRules {
		if entry.Method != method {
			continue
		}
		params, ok := matchPattern(splitPath(entry.Pattern), segments)
		if !ok {
			continue
		}
		rule := entry.Rule
		if rule == nil {
			rule = Admin
		}
		return rule, Target{PathParams: params}
	}
	return Admin, Target{}
}

// ContractProcedures lists every procedure path the contracts declare.
//
// It is derived from the generated descriptors rather than written out, so
// the table above can be checked against the contract itself: a rule attached
// to a procedure that no longer exists fails a test instead of becoming a
// guard nothing ever reaches.
func ContractProcedures() []string {
	files := []protoreflect.FileDescriptor{
		authv1.File_auth_proto,
		identityv1.File_identity_proto,
		systemv1.File_system_proto,
	}

	var procedures []string
	for _, file := range files {
		services := file.Services()
		for i := range services.Len() {
			service := services.Get(i)
			methods := service.Methods()
			for j := range methods.Len() {
				procedures = append(procedures,
					"/"+string(service.FullName())+"/"+string(methods.Get(j).Name()))
			}
		}
	}
	return procedures
}

// PublicProcedures is the set the bearer middleware answers without a caller.
// It is derived from the table above rather than kept beside it, so a
// procedure cannot be public for the authenticator and administrative for the
// guard.
func PublicProcedures() map[string]struct{} {
	public := make(map[string]struct{})
	for procedure, entry := range ProcedureRules {
		if IsPublic(entry.Rule) {
			public[procedure] = struct{}{}
		}
	}
	return public
}

// MatchRest answers the rule a REST request gets, with the target it names.
//
// The route is matched segment by segment, so a `{param}` stands for exactly
// one segment and a path parameter is read from the position the pattern puts
// it in. Matching here rather than reading chi's route context is deliberate:
// the middleware runs before the route is matched, and a guard that waited for
// the match would run after the handler was chosen.
//
// A route the tables do not name is administrative, the same default the
// procedure table applies.
func MatchRest(rules []RestEntry, method, path string) (Rule, Target) {
	segments := splitPath(path)
	for _, entry := range rules {
		if entry.Method != method {
			continue
		}
		params, ok := matchPattern(splitPath(entry.Pattern), segments)
		if !ok {
			continue
		}
		rule := entry.Rule
		if rule == nil {
			rule = Admin
		}
		return rule, Target{PathParams: params}
	}
	return Admin, Target{}
}

// CallerOf reads the caller an authenticator resolved.
//
// The authenticator answers the value the context carries, and this is the one
// place that value is turned back into a caller, so a middleware never has to
// know the concrete type. A value that is not a caller — a test's stub — is
// nobody, which every rule that needs a caller refuses.
func CallerOf(info any) *jwtutils.Caller {
	caller, _ := info.(*jwtutils.Caller)
	return caller
}

// matchPattern compares a route pattern against a path, answering the path
// parameters the pattern captured. A pattern with a different segment count
// cannot match, so `{id}` never stands for the rest of a path.
func matchPattern(pattern, path []string) (map[string]string, bool) {
	if len(pattern) != len(path) {
		return nil, false
	}
	var params map[string]string
	for i, segment := range pattern {
		if name, ok := paramName(segment); ok {
			if params == nil {
				params = make(map[string]string, 1)
			}
			params[name] = path[i]
			continue
		}
		if segment != path[i] {
			return nil, false
		}
	}
	return params, true
}

// paramName reads a `{name}` segment.
func paramName(segment string) (string, bool) {
	if len(segment) < 3 || !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
		return "", false
	}
	return segment[1 : len(segment)-1], true
}

// splitPath splits a URL path into its non-empty segments.
func splitPath(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
}
