package web

import "embed"

//go:embed email/*.tmpl
var EmailTemplates embed.FS

// ImagesDir is the on-disk location of the application images. In dev
// Vite serves public/images directly; in release the images land in
// web/output/images (copied from public/images by the Vite build).
// The path is relative to the working directory.
const ImagesDir = "web/output/images"
