package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Config selects the bucket, endpoint, and credentials.
type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
	Root            string
}

type s3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Storage builds an S3 backend with static credentials.
func NewS3Storage(cfg S3Config) (Store, error) {
	awsCfg, err := awscfg.LoadDefaultConfig(context.Background(),
		awscfg.WithRegion(cfg.Region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
		// Some S3-compatible servers reject streaming CRC payloads.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return &s3Storage{client: client, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Root, "/")}, nil
}

func (s *s3Storage) Type() string { return TypeS3 }

func (s *s3Storage) Save(ctx context.Context, p string, data io.Reader) error {
	// Buffer the stream because the SDK may read it more than once.
	buffered, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("storage: s3 read %q: %w", p, err)
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.objectKey(p)),
		Body:          bytes.NewReader(buffered),
		ContentLength: aws.Int64(int64(len(buffered))),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 put %q: %w", p, err)
	}
	return nil
}

func (s *s3Storage) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(p)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, fmt.Errorf("storage: s3 get %q: %w", p, err)
	}
	return resp.Body, aws.ToInt64(resp.ContentLength), nil
}

func (s *s3Storage) Delete(ctx context.Context, p string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(p)),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 delete %q: %w", p, err)
	}
	return nil
}

func (s *s3Storage) DeleteAll(ctx context.Context, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.objectKey(prefix)),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("storage: s3 list for delete: %w", err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		objects := make([]s3types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objects = append(objects, s3types.ObjectIdentifier{Key: obj.Key})
		}
		if _, err = s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		}); err != nil {
			return fmt.Errorf("storage: s3 batch delete: %w", err)
		}
	}
	return nil
}

func (s *s3Storage) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.objectKey(prefix)),
	})
	var objects []ObjectInfo
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("storage: s3 list: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			objects = append(objects, ObjectInfo{
				Path: s.pathFromKey(aws.ToString(obj.Key)),
				Size: aws.ToInt64(obj.Size),
			})
		}
	}
	return objects, nil
}

func (s *s3Storage) objectKey(p string) string {
	p = path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return s.prefix
	}
	if s.prefix == "" {
		return p
	}
	return s.prefix + "/" + p
}

func (s *s3Storage) pathFromKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return strings.TrimPrefix(key, s.prefix+"/")
}

func isS3NotFound(err error) bool {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		if apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey" {
			return true
		}
	}
	var missing *s3types.NoSuchKey
	return errors.As(err, &missing) || errors.Is(err, fs.ErrNotExist)
}
