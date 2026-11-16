package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Destination is somewhere backups are kept.
type Destination interface {
	String() string
	Put(ctx context.Context, name string, data []byte) error
	// List returns the names of the backups kept there.
	List(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, name string) error
}

// isBackupName reports whether a file name is one of ours:
// cafedesk-YYYY-MM-DD.sql.gz.
func isBackupName(n string) bool {
	d, ok := strings.CutPrefix(n, "cafedesk-")
	if !ok {
		return false
	}
	d, ok = strings.CutSuffix(d, ".sql.gz")
	return ok && len(d) == 10
}

// Dir keeps backups in a folder: a second disk or a USB drive in the shop,
// or a volume.
type Dir string

func (d Dir) String() string { return "folder " + string(d) }

func (d Dir) Put(_ context.Context, name string, data []byte) error {
	if err := os.MkdirAll(string(d), 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(string(d), name+".partial")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(string(d), name))
}

func (d Dir) List(context.Context) ([]string, error) {
	entries, err := os.ReadDir(string(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isBackupName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (d Dir) Delete(_ context.Context, name string) error {
	return os.Remove(filepath.Join(string(d), name))
}

// S3Config is an S3-compatible bucket: a Railway bucket, Cloudflare R2,
// Backblaze B2 or AWS S3.
type S3Config struct {
	Endpoint  string // https://t3.storageapi.dev
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string // "auto" on Railway
	PathStyle bool   // older Railway buckets, and some other services
	Prefix    string // a folder in the bucket, e.g. "backups/"
}

// Bucket keeps backups in an S3-compatible bucket.
type Bucket struct {
	cfg    S3Config
	client *minio.Client
}

// NewBucket checks a bucket's settings and returns it.
func NewBucket(cfg S3Config) (*Bucket, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("a bucket needs its endpoint, name, access key ID and secret access key")
	}
	host, secure := cfg.Endpoint, true
	if u, err := url.Parse(cfg.Endpoint); err == nil && u.Host != "" {
		host, secure = u.Host, u.Scheme != "http"
	}
	lookup := minio.BucketLookupDNS
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	return &Bucket{cfg, client}, nil
}

func (b *Bucket) String() string { return fmt.Sprintf("bucket %s/%s", b.cfg.Bucket, b.cfg.Prefix) }

func (b *Bucket) Put(ctx context.Context, name string, data []byte) error {
	_, err := b.client.PutObject(ctx, b.cfg.Bucket, b.cfg.Prefix+name, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/gzip"})
	return err
}

func (b *Bucket) List(ctx context.Context) ([]string, error) {
	var names []string
	for obj := range b.client.ListObjects(ctx, b.cfg.Bucket, minio.ListObjectsOptions{Prefix: b.cfg.Prefix}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if n := strings.TrimPrefix(obj.Key, b.cfg.Prefix); isBackupName(n) {
			names = append(names, n)
		}
	}
	return names, nil
}

func (b *Bucket) Delete(ctx context.Context, name string) error {
	return b.client.RemoveObject(ctx, b.cfg.Bucket, b.cfg.Prefix+name, minio.RemoveObjectOptions{})
}

// keepDaily is how many of the latest backups are kept; besides them, the
// first of each month's is kept for good.
const keepDaily = 30

// Prune deletes the backups past the latest keepDaily, except each month's
// first.
func Prune(ctx context.Context, d Destination) error {
	names, err := d.List(ctx)
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // newest first: the names hold the date
	for i, n := range names {
		if i < keepDaily || strings.HasSuffix(n, "-01.sql.gz") {
			continue
		}
		if err := d.Delete(ctx, n); err != nil {
			return err
		}
	}
	return nil
}
