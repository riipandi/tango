package signin

// ProviderPassword is the `provider` value a session row carries when the
// credential that created it was the account's password.
const ProviderPassword = "password"

// ProviderOneTimeAccess is the `provider` value a session row carries when a
// one-time access code created it. The values this column takes are the
// vocabulary of how a session was opened, so they are named beside each other
// here rather than beside the features that mint them.
const ProviderOneTimeAccess = "one_time_access"
