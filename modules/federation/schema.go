// Package federation is the optional identity provider surface this
// application exposes to other systems (OIDC/OAuth 2.0, SCIM,
// discovery). It has no mandatory core: it is exactly the features
// the composition root selects. Remove its registration line to
// build a pure internal-identity binary.
package federation
