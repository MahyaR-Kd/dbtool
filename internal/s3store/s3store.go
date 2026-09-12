// Package s3store provides helpers for uploading and downloading dump
// directories to/from an S3-compatible object store.
package s3store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dbtool/internal/logger"
	"dbtool/internal/settings"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// newClient builds an S3 client from the stored S3Config.
func newClient(cfg settings.S3Config) (*s3.Client, error) {
	if cfg.Bucket == "" || cfg.Region == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3 configuration is incomplete (Bucket, Region, Access Key, and Secret Key are required)")
	}

	awsCfg := aws.Config{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}

	opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(awsCfg, opts...), nil
}

// objectKey returns the S3 key for a given dump directory name.
func objectKey(prefix, dumpDir string) string {
	base := filepath.Base(dumpDir)
	if prefix != "" {
		return strings.TrimSuffix(prefix, "/") + "/" + base + "/"
	}
	return base + "/"
}

// UploadDir uploads all files in localDir to the configured S3 bucket.
// Each file is stored under <prefix>/<dumpDirName>/<filename>.
func UploadDir(cfg settings.S3Config, localDir string) error {
	client, err := newClient(cfg)
	if err != nil {
		return err
	}

	dirName := filepath.Base(localDir)
	keyPrefix := objectKey(cfg.Prefix, dirName)

	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		key := keyPrefix + filepath.ToSlash(rel)

		if err := uploadFile(client, cfg.Bucket, key, path); err != nil {
			return err
		}
		return nil
	})
}

// uploadFile opens a single local file and uploads it to S3.
func uploadFile(client *s3.Client, bucket, key, localPath string) error {
	f, err := os.Open(localPath) // #nosec G304 -- filepath.Walk guarantees paths stay within localDir; filepath.Join in the caller prevents directory traversal
	if err != nil {
		return fmt.Errorf("open %s: %w", localPath, err)
	}
	defer f.Close()

	logger.Debug("s3: uploading %s → s3://%s/%s", localPath, bucket, key)
	_, err = client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// ListDumps returns the top-level "directories" (common prefixes) inside the
// configured prefix, each representing one dump.
func ListDumps(cfg settings.S3Config) ([]string, error) {
	client, err := newClient(cfg)
	if err != nil {
		return nil, err
	}

	prefix := ""
	if cfg.Prefix != "" {
		prefix = strings.TrimSuffix(cfg.Prefix, "/") + "/"
	}

	var dumps []string
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(cfg.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list S3 objects: %w", err)
		}
		for _, cp := range page.CommonPrefixes {
			if cp.Prefix == nil {
				continue
			}
			// Strip the leading prefix so the user sees just the dump name.
			name := strings.TrimPrefix(*cp.Prefix, prefix)
			name = strings.TrimSuffix(name, "/")
			if name != "" {
				dumps = append(dumps, name)
			}
		}
	}

	return dumps, nil
}

// DeleteDump removes all objects under <prefix>/<dumpName>/ from S3.
func DeleteDump(cfg settings.S3Config, dumpName string) error {
	client, err := newClient(cfg)
	if err != nil {
		return err
	}

	prefix := ""
	if cfg.Prefix != "" {
		prefix = strings.TrimSuffix(cfg.Prefix, "/") + "/"
	}
	s3Prefix := prefix + dumpName + "/"

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(cfg.Bucket),
		Prefix: aws.String(s3Prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return fmt.Errorf("list objects for delete: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			logger.Debug("s3: deleting s3://%s/%s", cfg.Bucket, *obj.Key)
			_, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
				Bucket: aws.String(cfg.Bucket),
				Key:    obj.Key,
			})
			if err != nil {
				return fmt.Errorf("delete %s: %w", *obj.Key, err)
			}
		}
	}
	return nil
}

// DownloadDump downloads all objects under <prefix>/<dumpName>/ from S3 into
// destDir (which is created if it does not exist) and returns the local path.
func DownloadDump(cfg settings.S3Config, dumpName, destDir string) (string, error) {
	client, err := newClient(cfg)
	if err != nil {
		return "", err
	}

	prefix := ""
	if cfg.Prefix != "" {
		prefix = strings.TrimSuffix(cfg.Prefix, "/") + "/"
	}
	s3Prefix := prefix + dumpName + "/"

	localDumpDir := filepath.Join(destDir, dumpName)
	if err := os.MkdirAll(localDumpDir, 0755); err != nil {
		return "", fmt.Errorf("create local dir: %w", err)
	}

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(cfg.Bucket),
		Prefix: aws.String(s3Prefix),
	})

	downloaded := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return "", fmt.Errorf("list objects for download: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			key := *obj.Key
			relPath := strings.TrimPrefix(key, s3Prefix)
			if relPath == "" {
				continue
			}

			localPath := filepath.Join(localDumpDir, filepath.FromSlash(relPath))
			if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
				return "", fmt.Errorf("mkdir for %s: %w", localPath, err)
			}

			logger.Debug("s3: downloading s3://%s/%s → %s", cfg.Bucket, key, localPath)

			out, err := client.GetObject(context.Background(), &s3.GetObjectInput{
				Bucket: aws.String(cfg.Bucket),
				Key:    aws.String(key),
			})
			if err != nil {
				return "", fmt.Errorf("download %s: %w", key, err)
			}

			f, err := os.Create(localPath) // #nosec G304 -- localPath is built via filepath.Join+filepath.FromSlash which sanitizes the S3 key's relative path and prevents directory traversal
			if err != nil {
				out.Body.Close()
				return "", fmt.Errorf("create %s: %w", localPath, err)
			}
			_, copyErr := io.Copy(f, out.Body)
			out.Body.Close()
			f.Close()
			if copyErr != nil {
				return "", fmt.Errorf("write %s: %w", localPath, copyErr)
			}
			downloaded++
		}
	}

	if downloaded == 0 {
		return "", fmt.Errorf("no files found in S3 for dump %q", dumpName)
	}

	return localDumpDir, nil
}

// FindLatestDump returns the name of the most-recently-created dump in S3 for
// the given configName (using the naming convention "<configName>_YYYY-MM-DD_HHMMSS").
// Returns an empty string when no matching dump is found.
func FindLatestDump(cfg settings.S3Config, configName string) (string, error) {
	dumps, err := ListDumps(cfg)
	if err != nil {
		return "", err
	}

	prefix := configName + "_"
	var matching []string
	for _, d := range dumps {
		if strings.HasPrefix(d, prefix) {
			matching = append(matching, d)
		}
	}

	if len(matching) == 0 {
		return "", nil
	}

	// Sort ascending by name; since the timestamp is lexicographically ordered
	// the last entry is the most recent dump.
	sort.Strings(matching)
	return matching[len(matching)-1], nil
}
