// Package storage abstracts where a site's uploaded media lives. By default it
// is nil — media stays on the local filesystem exactly as before. When S3/R2 is
// configured (FRIENDO_S3_* env), FromEnv returns an S3 backend so uploads,
// deletes, and serving of assets/uploads/* go to object storage instead. Only
// uploaded media is offloaded; a site's templates and static assets stay on disk
// (Pongo2 reads templates from the filesystem).
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Backend stores and retrieves objects by key (e.g. "assets/uploads/abc.png").
type Backend interface {
	// Put writes an object. size may be -1 if unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Open returns a reader for an object plus its content type. The caller closes it.
	Open(ctx context.Context, key string) (io.ReadCloser, string, error)
	// Delete removes an object. Missing objects are not an error.
	Delete(ctx context.Context, key string) error
}

// Config describes an S3-compatible endpoint (works with Cloudflare R2, MinIO, S3, …).
type Config struct {
	Endpoint  string // host[:port], with or without scheme
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	Secure    bool
}

// s3Backend stores objects under prefix/<key> in one bucket, so many sites can
// share a bucket with per-site isolation.
type s3Backend struct {
	client *minio.Client
	bucket string
	prefix string
}

// NewS3 builds an S3 backend for a given per-site prefix. Used by FromEnv and tests.
func NewS3(cfg Config, prefix string) (Backend, error) {
	client, err := newS3Client(cfg)
	if err != nil {
		return nil, err
	}
	return &s3Backend{client: client, bucket: cfg.Bucket, prefix: prefix}, nil
}

func newS3Client(cfg Config) (*minio.Client, error) {
	endpoint := cfg.Endpoint
	secure := cfg.Secure
	if strings.HasPrefix(endpoint, "https://") {
		endpoint, secure = strings.TrimPrefix(endpoint, "https://"), true
	} else if strings.HasPrefix(endpoint, "http://") {
		endpoint, secure = strings.TrimPrefix(endpoint, "http://"), false
	}
	region := cfg.Region
	if region == "" {
		region = "auto" // Cloudflare R2
	}
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
	})
}

func (s *s3Backend) object(key string) string {
	return strings.TrimLeft(s.prefix+"/"+key, "/")
}

func (s *s3Backend) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.object(key), r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

func (s *s3Backend) Open(ctx context.Context, key string) (io.ReadCloser, string, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, s.object(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	// GetObject is lazy; Stat forces the fetch and surfaces a missing object.
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, "", err
	}
	return obj, info.ContentType, nil
}

func (s *s3Backend) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.object(key), minio.RemoveObjectOptions{})
}

// FromEnv returns an S3 backend for the site (prefixed by the site's folder name)
// when FRIENDO_S3_* is configured, or nil to mean "keep media on local disk". Env
// is read on each call — cheap, and predictable for tests / per-site config.
func FromEnv(siteDir string) (Backend, error) {
	cfg := Config{
		Endpoint:  os.Getenv("FRIENDO_S3_ENDPOINT"),
		Bucket:    os.Getenv("FRIENDO_S3_BUCKET"),
		AccessKey: os.Getenv("FRIENDO_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("FRIENDO_S3_SECRET_KEY"),
		Region:    os.Getenv("FRIENDO_S3_REGION"),
	}
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, nil
	}
	return NewS3(cfg, filepath.Base(siteDir))
}

// EnvStatus summarizes how media storage is configured from the environment, for
// a startup log line. It makes the disk-vs-R2 choice visible — and, crucially,
// warns on a partial FRIENDO_S3_* config that would otherwise silently fall back
// to local disk (media never reaching object storage, with no error).
func EnvStatus() string {
	keys := []string{"FRIENDO_S3_ENDPOINT", "FRIENDO_S3_BUCKET", "FRIENDO_S3_ACCESS_KEY", "FRIENDO_S3_SECRET_KEY"}
	var missing []string
	for _, k := range keys {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	switch len(missing) {
	case 0:
		return fmt.Sprintf("media: object storage → bucket=%q endpoint=%q", os.Getenv("FRIENDO_S3_BUCKET"), os.Getenv("FRIENDO_S3_ENDPOINT"))
	case len(keys):
		return "media: local disk (set FRIENDO_S3_* to use object storage)"
	default:
		return "media: WARNING incomplete FRIENDO_S3_* config — missing " + strings.Join(missing, ", ") + "; media is staying on local disk"
	}
}

// IsUpload reports whether a URL-relative asset path (under /assets/) refers to
// uploaded media (which may live in object storage) vs a static site asset.
func IsUpload(relPath string) bool {
	return strings.HasPrefix(relPath, "uploads/")
}
