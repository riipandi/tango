package config

import (
	"net/url"
	"strings"
)

// Supported values for the driver and format fields. A driver is opt-in: the
// default is always the dependency-free choice, so a fresh checkout runs on
// Postgres and the local filesystem alone.
//
// A key-value backend is named "kvstore" rather than after the product behind
// it, so the configuration does not have to change if that choice does.
const (
	CacheMemory   = "memory"
	CacheKV       = "kvstore"
	LogPretty     = "pretty"
	LogStructured = "structured"
	RateLimitDB   = "database"
	RateLimitKV   = "kvstore"
	SessionDB     = "database"
	SessionKV     = "kvstore"
	StorageLocal  = "local"
	StorageS3     = "s3"
)

// Sampler names OTEL.Tracing.Sampler accepts, mirroring the OpenTelemetry
// samplers: always records every trace, never records any, and the two ratio
// forms record a fraction of them.
const (
	OTELSamplerAlways = "always"
	OTELSamplerNever  = "never"
	OTELSamplerRatio  = "ratio"
	// OTELSamplerParentRatio records a fraction of the traces that start fresh
	// and follows the decision of a parent for the rest, so a service that
	// receives a sampled request keeps the trace whole.
	OTELSamplerParentRatio = "parent_ratio"
)

// OTELSamplers returns every accepted sampler name, in the order the
// documentation lists them.
func OTELSamplers() []string {
	return []string{OTELSamplerAlways, OTELSamplerNever, OTELSamplerRatio, OTELSamplerParentRatio}
}

// usesOTELRatio reports whether a sampler reads Tracing.Ratio.
//
// OTEL.Tracing.Sampler accepts these two ratios, which are the ones that read
// Tracing.Ratio. Every other sampler ignores it, so Validate refuses a ratio
// that nothing would read rather than leaving a user with a value that looks
// applied and is not.
func usesOTELRatio(sampler string) bool {
	return sampler == OTELSamplerRatio || sampler == OTELSamplerParentRatio
}

// Protocol names OTEL.Protocol accepts, spelled the way the OpenTelemetry
// specification spells them so a value can be copied from its documentation.
const (
	// OTELProtocolGRPC is the protobuf payload over gRPC, on the protocol's own
	// port (4317).
	OTELProtocolGRPC = "grpc"
	// OTELProtocolHTTPProtobuf is the protobuf payload over HTTP, on port 4318.
	// It is the default here because it is the protocol an HTTP deployment can
	// put behind the same proxy, TLS terminator, and firewall rule as the rest
	// of its traffic.
	OTELProtocolHTTPProtobuf = "http/protobuf"
	// OTELProtocolHTTPJSON is the JSON payload over HTTP. It is readable with
	// curl, which is what makes it useful against a collector being debugged.
	//
	// The Go exporters implement it unevenly — only the trace exporter encodes
	// JSON — so Validate refuses it for metrics and logs rather than sending
	// them as protobuf to a collector expecting JSON.
	OTELProtocolHTTPJSON = "http/json"
)

// OTELProtocols returns every accepted protocol name.
//
// The three are the whole set the specification defines. A name outside it is
// refused rather than mapped to a default, because a deployment that asked for
// one protocol and silently got another has telemetry its collector may reject
// without saying so.
func OTELProtocols() []string {
	return []string{OTELProtocolGRPC, OTELProtocolHTTPProtobuf, OTELProtocolHTTPJSON}
}

// UsesHTTP reports whether the protocol travels over HTTP, which is what decides
// whether a signal's path and the endpoint's own path mean anything. A gRPC
// service is addressed by host and port alone.
func UsesHTTP(protocol string) bool {
	return protocol != OTELProtocolGRPC
}

// Compression names OTEL.Compression accepts.
const (
	// OTELCompressionGzip compresses every export. It is the protocol's own
	// default and what a collector across a network wants.
	OTELCompressionGzip = "gzip"
	// OTELCompressionNone sends the payload uncompressed, which saves the CPU
	// when the collector is on the same host or the same local network.
	OTELCompressionNone = "none"
)

// OTELCompressions returns every accepted compression name.
func OTELCompressions() []string {
	return []string{OTELCompressionGzip, OTELCompressionNone}
}

// Log transport names, the values Log.Transport accepts. Each one is a sink the
// logger builds; naming it in the list is what switches it on.
const (
	// LogTransportConsole writes to the terminal. It is the default and the
	// only sink a fresh checkout needs.
	LogTransportConsole = "console"
	// LogTransportFile writes one JSON object per line to a rotating file under
	// storage.local_path + /logs.
	LogTransportFile = "file"
	// LogTransportOTLP ships entries to an OpenTelemetry collector.
	LogTransportOTLP = "otlp"
)

// LogTransports returns every accepted transport name, in the order the
// documentation lists them. It is what a validation message names, so a rejected
// value is answered with the list rather than with one example.
func LogTransports() []string {
	return []string{LogTransportConsole, LogTransportFile, LogTransportOTLP}
}

// CollectorEndpoint returns the collector address in the form the configured
// protocol's exporter wants.
//
// The two forms differ because the two protocols address a service differently.
// An HTTP exporter wants the URL it can dial, scheme included. A gRPC exporter
// wants host:port and takes the scheme from the credentials instead, so an
// endpoint written as a URL is reduced to its host and port: the same value
// serves both protocols, which is what one shared endpoint means.
func (c Config) CollectorEndpoint() string {
	if UsesHTTP(c.OTEL.Protocol) {
		return c.OTEL.Endpoint
	}
	parsed, err := url.Parse(c.OTEL.Endpoint)
	if err != nil || parsed.Host == "" {
		return c.OTEL.Endpoint
	}
	return parsed.Host
}

// CollectorSecure reports whether the collector connection uses TLS.
//
// It is read from the endpoint's own scheme, so there is no second setting that
// could disagree with the address: an https URL is TLS for an HTTP protocol, and
// an https:// URL is what turns it on for gRPC, whose address carries no scheme
// of its own. A bare host:port is plaintext, which is what a collector on the
// same host or the same private network wants.
func (c Config) CollectorSecure() bool {
	parsed, err := url.Parse(c.OTEL.Endpoint)
	return err == nil && parsed.Scheme == "https"
}

// JWTAlgorithms are the signature algorithms auth.jwt_algorithm accepts. They
// are listed here rather than read from the JWS library so this package stays
// free of that dependency; a test in modules/identity/jwks asserts the two
// lists agree, so a library that grows an algorithm fails there instead of
// accepting a value this list rejects.
var JWTAlgorithms = []string{
	"HS256", "HS384", "HS512",
	"RS256", "RS384", "RS512",
	"ES256", "ES256K", "ES384", "ES512",
	"PS256", "PS384", "PS512",
	"EdDSA", "Ed25519",
}

// IsHMACAlgorithm reports whether an algorithm is one of the symmetric ones.
// It is what tells the HMAC half of the dual stack from the key-pair half.
func IsHMACAlgorithm(algorithm string) bool {
	return strings.HasPrefix(algorithm, "HS")
}
