package storage

// TODO: S3 backend (prod).
//
// aws-sdk-go-v2 (config + s3): bucket/region/endpoint from config
// (custom endpoint = MinIO-compatible). Put streams PutObject;
// Open maps NoSuchKey to ErrNotFound; Delete is DeleteObject.
// Presigned URLs later go behind a Presigner capability, not by
// widening Store.
