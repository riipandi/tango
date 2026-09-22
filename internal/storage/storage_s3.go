package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	awshttp "github.com/aws/smithy-go/transport/http"

	"github.com/riipandi/tango/internal/config"
)

// S3 is the object-store backend: every chunk is one object under the
// configured path prefix, addressed by the same layout the local driver
// lays out. Any service speaking the S3 protocol answers — the endpoint,
// the path style, and the credentials are the configuration's, so AWS,
// MinIO, and Silo need no code between them.
type S3 struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3 builds the backend from the storage.s3 settings. The client is
// opened here because it names one bucket the driver alone reads: unlike
// the shared Valkey client, no second feature ever borrows it.
func NewS3(cfg config.S3) (*S3, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.AccessKeySecret,
			"", // session token: a long-lived deployment key has none
		)),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 client: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.EndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})
	return &S3{
		client: client,
		bucket: cfg.BucketName,
		prefix: strings.TrimPrefix(cfg.PathPrefix, "/"),
	}, nil
}

// HasChunks answers the whole batch with one listing per two-hex-character
// prefix the chunks share: a file's chunks spread across at most as many
// prefixes as its first bytes do, so a sync probes the backend with a
// handful of LISTs instead of one HEAD per chunk.
func (s *S3) HasChunks(ctx context.Context, hashes []string) (map[string]bool, error) {
	found := make(map[string]bool, len(hashes))
	prefixes := make(map[string][]string)
	for _, hash := range hashes {
		prefix := hash[:2]
		prefixes[prefix] = append(prefixes[prefix], hash)
	}

	for prefix, group := range prefixes {
		listed := make(map[string]bool, len(group))
		paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(s.prefix + "chunks/" + prefix + "/"),
		})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("storage: probe chunks %s/*: %w", prefix, err)
			}
			for _, object := range page.Contents {
				if hash, ok := s.chunkHash(aws.ToString(object.Key)); ok {
					listed[hash] = true
				}
			}
		}
		for _, hash := range group {
			found[hash] = listed[hash]
		}
	}
	return found, nil
}

// PutChunk stores the chunk as one object. Writing the same hash twice
// writes the same bytes; the object store needs no temp file, a PUT arrives
// whole or not at all.
func (s *S3) PutChunk(ctx context.Context, hash string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.chunkKey(hash)),
		Body:   strings.NewReader(string(data)),
	})
	if err != nil {
		return fmt.Errorf("storage: put chunk %s: %w", hash, err)
	}
	return nil
}

// GetChunk reads the chunk's object into dst.
func (s *S3) GetChunk(ctx context.Context, dst []byte, hash string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.chunkKey(hash)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return dst, fmt.Errorf("storage: chunk %s: %w", hash, ErrNotFound)
		}
		return dst, fmt.Errorf("storage: get chunk %s: %w", hash, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return dst, fmt.Errorf("storage: get chunk %s: %w", hash, err)
	}
	return append(dst, data...), nil
}

// DeleteChunk removes the chunk's object. A delete of a key already gone
// answers success in the protocol, so the idempotence is the backend's.
func (s *S3) DeleteChunk(ctx context.Context, hash string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.chunkKey(hash)),
	})
	if err != nil {
		return fmt.Errorf("storage: delete chunk %s: %w", hash, err)
	}
	return nil
}

// ListChunks pages the chunk prefix and calls fn for every object whose key
// ends in a hash. Objects at other shapes under the prefix — a manifest the
// operator parked, a stray — are skipped, the way the local driver skips a
// name that is not a hash.
func (s *S3) ListChunks(ctx context.Context, fn func(hash string) error) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix + "chunks/"),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("storage: list chunks: %w", err)
		}
		for _, object := range page.Contents {
			hash, ok := s.chunkHash(aws.ToString(object.Key))
			if !ok {
				continue
			}
			if err := fn(hash); err != nil {
				return err
			}
		}
	}
	return nil
}

// chunkKey is where one chunk lives: the path prefix, the chunk layout.
func (s *S3) chunkKey(hash string) string {
	return s.prefix + ChunkName(hash)
}

// chunkHash reads a hash out of an object key listed under the chunk
// prefix, the inverse of chunkKey with the prefix already removed.
func (s *S3) chunkHash(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, s.prefix+"chunks/")
	if !ok {
		return "", false
	}
	_, hash, ok := strings.Cut(rest, "/")
	if !ok || !chunkHashPattern.MatchString(hash) {
		return "", false
	}
	return hash, true
}

// isS3NotFound reports the not-found shape the protocol answers a HEAD and
// a GET with. The errors are smithy API errors carrying an HTTP status, not
// sentinel values, so the status is what is checked.
func isS3NotFound(err error) bool {
	if apiErr, ok := errors.AsType[*awshttp.ResponseError](err); ok {
		return apiErr.HTTPStatusCode() == 404
	}
	return false
}
