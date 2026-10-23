// Package storage stores attachment content in S3-compatible object storage (AWS S3, Garage,
// SeaweedFS, MinIO) or, when none is configured, in PostgreSQL.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Open returns the content, or a pre-signed URL to redirect to (S3).
	Open(ctx context.Context, key, filename string) (io.ReadCloser, *url.URL, error)
	Delete(ctx context.Context, key string) error
	Name() string
}

func New(ctx context.Context, cfg config.S3, pool *pgxpool.Pool) (Store, error) {
	if !cfg.Enabled() {
		return &pgStore{q: dbq.New(pool)}, nil
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &s3Store{client: client, bucket: cfg.Bucket}, nil
}

type s3Store struct {
	client *minio.Client
	bucket string
}

func (s *s3Store) Name() string { return "s3" }

func (s *s3Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *s3Store) Open(ctx context.Context, key, filename string) (io.ReadCloser, *url.URL, error) {
	params := url.Values{}
	params.Set("response-content-disposition", fmt.Sprintf("attachment; filename=%q", filename))
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, 5*time.Minute, params)
	return nil, u, err
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

type pgStore struct{ q *dbq.Queries }

func (p *pgStore) Name() string { return "postgres" }

func (p *pgStore) Put(ctx context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return p.q.PutBlob(ctx, dbq.PutBlobParams{Key: key, Data: b})
}

func (p *pgStore) Open(ctx context.Context, key, _ string) (io.ReadCloser, *url.URL, error) {
	b, err := p.q.GetBlob(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil, nil
}

func (p *pgStore) Delete(ctx context.Context, key string) error { return p.q.DeleteBlob(ctx, key) }
