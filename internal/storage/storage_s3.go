package storage

// TODO: S3 backend (production).
//
// aws-sdk-go-v2 client (s3 + config packages) with bucket,
// region, and endpoint from config (e.g. storage.s3.*; custom
// endpoint enables MinIO/compatible stores). Put streams the
// reader via PutObject; Open uses GetObject and maps
// apierr NoSuchKey to ErrNotFound; Delete calls DeleteObject.
//
// Later, if presigned URL upload/download lands, extend the
// contract behind a separate Presigner capability interface
// instead of widening Store for every backend.
