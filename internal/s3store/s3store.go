// Package s3store provides helpers for uploading and downloading dump
// directories to/from an S3-compatible object store.
package s3store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dbtool/internal/archivecrypt"
	"dbtool/internal/backupstate"
	"dbtool/internal/logger"
	"dbtool/internal/settings"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// encryptedSuffix marks an S3 object as archivecrypt-encrypted (appended
// to its key on upload) so DownloadDump knows to decrypt it — and strip
// the suffix back off — on the way down.
const encryptedSuffix = ".enc"

// TestConnection verifies the configured credentials can write, read, and
// delete a small object. The probe is isolated under the configured prefix.
func TestConnection(cfg settings.S3Config) error {
	client, err := newClient(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	key := ".dbtool-test-" + fmt.Sprint(time.Now().UnixNano())
	if prefix != "" {
		key = prefix + "/" + key
	}
	want := "dbtool s3 connection test\n"
	if _, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key), Body: strings.NewReader(want)}); err != nil {
		return fmt.Errorf("write test object: %w", err)
	}
	deleted := false
	defer func() {
		if !deleted {
			_, _ = client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)})
		}
	}()
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("read test object: %w", err)
	}
	got, readErr := io.ReadAll(out.Body)
	closeErr := out.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read test object body: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close test object body: %w", closeErr)
	}
	if string(got) != want {
		return fmt.Errorf("test object content did not match")
	}
	if _, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete test object: %w", err)
	}
	deleted = true
	return nil
}

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

	if !backupstate.IsComplete(localDir) {
		return fmt.Errorf("backup is not complete: %s", localDir)
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(keyPrefix + backupstate.Pending), Body: strings.NewReader("pending\n")}); err != nil {
		return err
	}
	if err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Name() == backupstate.Complete || info.Name() == backupstate.Pending {
			return nil
		}

		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		key := keyPrefix + filepath.ToSlash(rel)
		if cfg.EncryptionEnabled() {
			key += encryptedSuffix
		}

		if err := uploadFile(client, cfg, key, path); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(keyPrefix + backupstate.Complete), Body: strings.NewReader("complete\n")}); err != nil {
		return err
	}
	_, err = client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(keyPrefix + backupstate.Pending)})
	return err
}

// uploadFile opens a single local file and uploads it to S3, encrypting it
// first (into a sibling temp file, then uploaded from there) when cfg has
// an encryption password configured. Encrypting to a temp file rather
// than streaming through a pipe keeps the upload body a plain *os.File —
// the same shape the unencrypted path already uses — so the S3 SDK can
// determine its length up front exactly as it always has, instead of
// relying on less certain streaming-body behavior for a large upload.
func uploadFile(client *s3.Client, cfg settings.S3Config, key, localPath string) error {
	f, err := os.Open(localPath) // #nosec G304 -- filepath.Walk guarantees paths stay within localDir; filepath.Join in the caller prevents directory traversal
	if err != nil {
		return fmt.Errorf("open %s: %w", localPath, err)
	}
	defer f.Close()

	body := io.Reader(f)
	if cfg.EncryptionEnabled() {
		encPath := localPath + encryptedSuffix + ".tmp"
		encFile, err := os.Create(encPath) // #nosec G304 -- derived from localPath, which filepath.Walk guarantees stays within the trusted local dump directory
		if err != nil {
			return fmt.Errorf("create temp encrypted file for %s: %w", localPath, err)
		}
		defer os.Remove(encPath)

		if err := archivecrypt.EncryptStream(encFile, f, cfg.EncryptionPassword); err != nil {
			encFile.Close()
			return fmt.Errorf("encrypt %s: %w", localPath, err)
		}
		if err := encFile.Close(); err != nil {
			return fmt.Errorf("close temp encrypted file for %s: %w", localPath, err)
		}

		encFileForUpload, err := os.Open(encPath) // #nosec G304 -- encPath was just created above from a trusted local path
		if err != nil {
			return fmt.Errorf("reopen encrypted %s for upload: %w", localPath, err)
		}
		defer encFileForUpload.Close()
		body = encFileForUpload
	}

	logger.Debug("s3: uploading %s → s3://%s/%s", localPath, cfg.Bucket, key)
	_, err = client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(key),
		Body:   body,
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
				complete, err := remoteDumpComplete(client, cfg.Bucket, *cp.Prefix)
				if err != nil {
					return nil, err
				}
				if complete {
					dumps = append(dumps, name)
				}
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
	if dumpName == "." || dumpName == ".." || filepath.Base(dumpName) != dumpName || !filepath.IsLocal(dumpName) {
		return "", fmt.Errorf("invalid dump name %q", dumpName)
	}
	client, err := newClient(cfg)
	if err != nil {
		return "", err
	}

	prefix := ""
	if cfg.Prefix != "" {
		prefix = strings.TrimSuffix(cfg.Prefix, "/") + "/"
	}
	s3Prefix := prefix + dumpName + "/"

	complete, err := remoteDumpComplete(client, cfg.Bucket, s3Prefix)
	if err != nil {
		return "", err
	}
	if !complete {
		return "", fmt.Errorf("S3 backup %q is not complete", dumpName)
	}
	localDumpDir := filepath.Join(destDir, dumpName)
	if err := os.MkdirAll(localDumpDir, 0755); err != nil {
		return "", fmt.Errorf("create local dir: %w", err)
	}

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(cfg.Bucket),
		Prefix: aws.String(s3Prefix),
	})

	root, err := os.OpenRoot(localDumpDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
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

			encrypted := strings.HasSuffix(relPath, encryptedSuffix)
			if encrypted {
				relPath = strings.TrimSuffix(relPath, encryptedSuffix)
			}

			relative, err := safeObjectPath(relPath)
			if err != nil {
				return "", err
			}
			localPath := filepath.Join(localDumpDir, relative)
			if err := mkdirParents(root, filepath.Dir(relative)); err != nil {
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

			f, err := root.Create(relative)
			if err != nil {
				out.Body.Close()
				return "", fmt.Errorf("create %s: %w", localPath, err)
			}
			var copyErr error
			switch {
			case encrypted && cfg.EncryptionPassword == "":
				copyErr = fmt.Errorf("object %q is encrypted but no S3 encryption password is configured", key)
			case encrypted:
				copyErr = archivecrypt.DecryptStream(f, out.Body, cfg.EncryptionPassword)
			default:
				_, copyErr = io.Copy(f, out.Body)
			}
			out.Body.Close()
			closeErr := f.Close()
			if copyErr == nil {
				copyErr = closeErr
			}
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

func safeObjectPath(path string) (string, error) {
	relative := filepath.FromSlash(path)
	if !filepath.IsLocal(relative) || filepath.Clean(relative) == "." {
		return "", fmt.Errorf("unsafe S3 object path %q", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe S3 object path %q", path)
		}
	}
	return relative, nil
}

// Root operations also prevent pre-existing symlinks from escaping the download.
func mkdirParents(root *os.Root, dir string) error {
	if dir == "." {
		return nil
	}
	path := ""
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		if err := root.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func remoteObjectExists(client *s3.Client, bucket, key string) (bool, error) {
	_, err := client.HeadObject(context.Background(), &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err == nil {
		return true, nil
	}
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) && response.HTTPStatusCode() == 404 {
		return false, nil
	}
	return false, err
}

func remoteDumpComplete(client *s3.Client, bucket, prefix string) (bool, error) {
	pending, err := remoteObjectExists(client, bucket, prefix+backupstate.Pending)
	if err != nil || pending {
		return false, err
	}
	complete, err := remoteObjectExists(client, bucket, prefix+backupstate.Complete)
	if err != nil || complete {
		return complete, err
	}
	for _, name := range []string{"metadata", "metadata.enc"} {
		exists, err := remoteObjectExists(client, bucket, prefix+name)
		if err != nil || exists {
			return exists, err
		}
	}
	return false, nil
}
