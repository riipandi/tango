package discovery

// discoveryDocument is the OIDC Discovery 1.0 document we expose.
type discoveryDocument struct {
	Issuer                             string   `json:"issuer"`
	AuthorizationEndpoint              string   `json:"authorization_endpoint"`
	TokenEndpoint                      string   `json:"token_endpoint"`
	UserInfoEndpoint                   string   `json:"userinfo_endpoint"`
	EndSessionEndpoint                 string   `json:"end_session_endpoint"`
	IntrospectionEndpoint              string   `json:"introspection_endpoint"`
	PushedAuthorizationRequestEndpoint string   `json:"pushed_authorization_request_endpoint"`
	DeviceAuthorizationEndpoint        string   `json:"device_authorization_endpoint"`
	JWKSURI                            string   `json:"jwks_uri"`
	ScopesSupported                    []string `json:"scopes_supported"`
	ResponseTypesSupported             []string `json:"response_types_supported"`
	ResponseModesSupported             []string `json:"response_modes_supported"`
	GrantTypesSupported                []string `json:"grant_types_supported"`
	SubjectTypesSupported              []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported   []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethodsSupported  []string `json:"token_endpoint_auth_methods_supported"`
	IntrospectionEndpointAuthMethods   []string `json:"introspection_endpoint_auth_methods_supported"`
	PromptValuesSupported              []string `json:"prompt_values_supported"`
	ClaimsSupported                    []string `json:"claims_supported"`
	CodeChallengeMethodsSupported      []string `json:"code_challenge_methods_supported"`
	RequestObjectSigningAlgValues      []string `json:"request_object_signing_alg_values_supported"`
	AuthorizationResponseIssParameter  bool     `json:"authorization_response_iss_parameter_supported"`
	ClientIDMetadataDocumentSupported  bool     `json:"client_id_metadata_document_supported"`
	RequestURIParameterSupported       bool     `json:"request_uri_parameter_supported"`
	RequirePushedAuthorizationRequests bool     `json:"require_pushed_authorization_requests"`
}

func newDiscoveryDocument(issuer string) discoveryDocument {
	return discoveryDocument{
		Issuer:                             issuer,
		AuthorizationEndpoint:              issuer + AuthorizeEndpoint,
		TokenEndpoint:                      issuer + TokenEndpoint,
		UserInfoEndpoint:                   issuer + UserInfoEndpoint,
		EndSessionEndpoint:                 issuer + EndSessionEndpoint,
		IntrospectionEndpoint:              issuer + IntrospectionEndpoint,
		PushedAuthorizationRequestEndpoint: issuer + PushedAuthorizationRequestEndpoint,
		DeviceAuthorizationEndpoint:        issuer + DeviceAuthorizationEndpoint,
		JWKSURI:                            issuer + JWKSURI,
		ScopesSupported:                    []string{"openid", "email", "profile", "groups"},
		ResponseTypesSupported:             []string{"code"},
		ResponseModesSupported:             []string{"query", "fragment", "form_post"},
		GrantTypesSupported:                []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"},
		SubjectTypesSupported:              []string{"public"},
		IDTokenSigningAlgValuesSupported:   []string{"RS256", "ES256"},
		TokenEndpointAuthMethodsSupported:  []string{"client_secret_basic", "client_secret_post", "none"},
		IntrospectionEndpointAuthMethods:   []string{"client_secret_basic", "Bearer"},
		PromptValuesSupported:              []string{"none", "login", "consent", "select_account"},
		ClaimsSupported:                    []string{"sub", "iss", "aud", "exp", "iat", "auth_time", "email", "email_verified", "name", "preferred_username", "groups"},
		CodeChallengeMethodsSupported:      []string{"S256"},
		RequestObjectSigningAlgValues:      []string{"none"},
		AuthorizationResponseIssParameter:  true,
		ClientIDMetadataDocumentSupported:  false,
		RequestURIParameterSupported:       true,
		RequirePushedAuthorizationRequests: false,
	}
}
