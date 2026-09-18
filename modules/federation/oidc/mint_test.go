package oidc

import (
	"testing"
	"time"

	"github.com/riipandi/tango/modules/identity/user"
)

// TestMintTokensDirect isolates the token-minting path to surface
// store errors verbatim.
func TestMintTokensDirect(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	userStore := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, userStore, stamp())
	client := clientFixture(ctx, t, store, "rp-"+stamp())

	response, err := service.mintTokens(ctx, client, createdUser.String(), "openid email profile groups", "n-1", "", seedFamily(NewID().String(), "sid-1", "password", time.Now().UTC()))
	if err != nil {
		t.Fatalf("mintTokens: %v", err)
	}
	if response.AccessToken == "" || response.IDToken == "" || response.RefreshToken == "" {
		t.Fatalf("incomplete token response: %+v", response)
	}
}
