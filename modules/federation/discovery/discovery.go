package discovery

// discoveryDocument is the OIDC Discovery 1.0 document we expose.
type discoveryDocument struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserInfoEndpoint                  string   `json:"userinfo_endpoint"`
	EndSessionEndpoint                string   `json:"end_session_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ClaimsSupported                   []string `json:"claims_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	ClaimsParameterSupported          bool     `json:"claims_parameter_supported"`
	RequestParameterSupported         bool     `json:"request_parameter_supported"`
}

func newDiscoveryDocument(issuer string) discoveryDocument {
	return discoveryDocument{
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + AuthorizeEndpoint,
		TokenEndpoint:                     issuer + TokenEndpoint,
		UserInfoEndpoint:                  issuer + UserInfoEndpoint,
		EndSessionEndpoint:                issuer + EndSessionEndpoint,
		JWKSURI:                           issuer + JWKSURI,
		ScopesSupported:                   []string{"openid", "email", "profile", "groups"},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"RS256", "ES256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post", "none"},
		ClaimsSupported:                   []string{"sub", "iss", "aud", "exp", "iat", "auth_time", "email", "email_verified", "name", "preferred_username", "groups"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ClaimsParameterSupported:          false,
		RequestParameterSupported:         false,
	}
}
