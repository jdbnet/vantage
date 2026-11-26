package s3

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jdbnet/vantage/internal/explorer"
)

type S3Explorer struct {
	cfg    explorer.Config
	client *s3.Client
}

func New(c explorer.Config) *S3Explorer {
	return &S3Explorer{cfg: c}
}

func (e *S3Explorer) Connect() error {
	region := e.cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	creds := credentials.NewStaticCredentialsProvider(e.cfg.Username, e.cfg.Secret, "")

	optFns := []func(*config.LoadOptions) error{
		config.WithCredentialsProvider(creds),
		config.WithRegion(region),
	}

	awsCfg, err := config.LoadDefaultConfig(context.TODO(), optFns...)
	if err != nil {
		return err
	}

	clientOpts := func(o *s3.Options) {
		if e.cfg.Host != "" {
			host := e.cfg.Host
			if !strings.HasPrefix(host, "http") {
				host = "https://" + host
			}
			if e.cfg.Port > 0 {
				host = fmt.Sprintf("%s:%d", host, e.cfg.Port)
			}
			o.BaseEndpoint = aws.String(host)
		}
		o.UsePathStyle = e.cfg.PathStyle
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	}

	e.client = s3.NewFromConfig(awsCfg, clientOpts)
	return nil
}

func (e *S3Explorer) Disconnect() error {
	return nil
}

func (e *S3Explorer) getBucketAndPath(path string) (string, string, string, error) {
	if e.cfg.Bucket != "" {
		return e.cfg.Bucket, strings.TrimPrefix(path, "/"), "", nil
	}

	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "", "", "", fmt.Errorf("no bucket specified")
	}

	parts := strings.SplitN(path, "/", 2)
	bucket := parts[0]
	subpath := ""
	if len(parts) > 1 {
		subpath = parts[1]
	}
	return bucket, subpath, bucket, nil
}

func (e *S3Explorer) ListDir(path string) ([]explorer.FileEntry, error) {
	if e.cfg.Bucket == "" && (path == "" || path == "/") {
		result, err := e.client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
		if err != nil {
			return nil, err
		}
		var entries []explorer.FileEntry
		for _, b := range result.Buckets {
			entries = append(entries, explorer.FileEntry{
				Name:        *b.Name,
				Path:        *b.Name,
				Modified:    b.CreationDate.Format(time.RFC3339),
				IsDir:       true,
				Permissions: "bucket",
			})
		}
		return entries, nil
	}

	bucket, subpath, bucketName, err := e.getBucketAndPath(path)
	if err != nil {
		return nil, err
	}

	prefix := subpath
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	}

	result, err := e.client.ListObjectsV2(context.TODO(), input)
	if err != nil {
		return nil, err
	}

	var entries []explorer.FileEntry
	for _, cp := range result.CommonPrefixes {
		name := strings.TrimSuffix(*cp.Prefix, "/")
		name = filepath.Base(name)

		fullPath := *cp.Prefix
		if e.cfg.Bucket == "" {
			fullPath = bucketName + "/" + *cp.Prefix
		}

		entries = append(entries, explorer.FileEntry{
			Name:        name,
			Path:        fullPath,
			IsDir:       true,
			Permissions: "dir",
		})
	}

	for _, obj := range result.Contents {
		if *obj.Key == prefix {
			continue
		}
		name := filepath.Base(*obj.Key)

		fullPath := *obj.Key
		if e.cfg.Bucket == "" {
			fullPath = bucketName + "/" + *obj.Key
		}

		entries = append(entries, explorer.FileEntry{
			Name:        name,
			Path:        fullPath,
			Size:        *obj.Size,
			Modified:    obj.LastModified.Format(time.RFC3339),
			IsDir:       false,
			Permissions: "file",
		})
	}
	return entries, nil
}

func (e *S3Explorer) Stat(path string) (explorer.FileEntry, error) {
	if e.cfg.Bucket == "" && (path == "" || path == "/") {
		return explorer.FileEntry{
			Name: "/", Path: "/", IsDir: true,
		}, nil
	}

	bucket, subpath, bucketName, err := e.getBucketAndPath(path)
	if err != nil {
		return explorer.FileEntry{}, err
	}

	if subpath == "" {
		return explorer.FileEntry{
			Name: bucket, Path: bucketName, IsDir: true,
		}, nil
	}

	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(subpath),
	}
	head, err := e.client.HeadObject(context.TODO(), input)
	if err != nil {
		return explorer.FileEntry{
			Name:  filepath.Base(subpath),
			Path:  path,
			IsDir: true,
		}, nil
	}

	return explorer.FileEntry{
		Name:     filepath.Base(subpath),
		Path:     path,
		Size:     *head.ContentLength,
		Modified: head.LastModified.Format(time.RFC3339),
		IsDir:    false,
	}, nil
}

func (e *S3Explorer) MkDir(path string) error {
	bucket, subpath, _, err := e.getBucketAndPath(path)
	if err != nil {
		return err
	}

	if subpath == "" {
		_, err := e.client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
			Bucket: aws.String(bucket),
		})
		return err
	}

	if !strings.HasSuffix(subpath, "/") {
		subpath += "/"
	}
	_, err = e.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(subpath),
		Body:   strings.NewReader(""),
	})
	return err
}

func (e *S3Explorer) Delete(path string) error {
	bucket, subpath, _, err := e.getBucketAndPath(path)
	if err != nil {
		return err
	}

	if subpath == "" {
		_, err := e.client.DeleteBucket(context.TODO(), &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
		return err
	}

	// Try to delete the exact object (might be a file or a folder marker)
	_, err = e.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(subpath),
	})
	if err != nil {
		// Ignore error; it might just be a prefix with no exact marker
	}

	// Also treat subpath as a folder prefix and delete all matching objects
	prefix := subpath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var continuationToken *string
	for {
		listInput := &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		}
		listOutput, err := e.client.ListObjectsV2(context.TODO(), listInput)
		if err != nil {
			return err
		}

		if len(listOutput.Contents) > 0 {
			var objectsToDelete []types.ObjectIdentifier
			for _, obj := range listOutput.Contents {
				objectsToDelete = append(objectsToDelete, types.ObjectIdentifier{
					Key: obj.Key,
				})
			}

			_, err = e.client.DeleteObjects(context.TODO(), &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &types.Delete{
					Objects: objectsToDelete,
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				return err
			}
		}

		if listOutput.IsTruncated != nil && *listOutput.IsTruncated {
			continuationToken = listOutput.NextContinuationToken
		} else {
			break
		}
	}

	return nil
}

func (e *S3Explorer) Rename(src, dst string) error {
	bucket1, subpath1, _, err := e.getBucketAndPath(src)
	if err != nil {
		return err
	}
	bucket2, subpath2, _, err := e.getBucketAndPath(dst)
	if err != nil {
		return err
	}

	if bucket1 != bucket2 {
		return fmt.Errorf("cross-bucket rename not supported")
	}

	_, err = e.client.CopyObject(context.TODO(), &s3.CopyObjectInput{
		Bucket:     aws.String(bucket2),
		CopySource: aws.String(fmt.Sprintf("%s/%s", bucket1, subpath1)),
		Key:        aws.String(subpath2),
	})
	if err != nil {
		return err
	}

	_, err = e.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket1),
		Key:    aws.String(subpath1),
	})
	return err
}

func (e *S3Explorer) ReadFile(path string) (io.ReadCloser, error) {
	return e.ReadRange(path, 0, 0)
}

func (e *S3Explorer) ReadRange(path string, start, length int64) (io.ReadCloser, error) {
	bucket, subpath, _, err := e.getBucketAndPath(path)
	if err != nil {
		return nil, err
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(subpath),
	}
	if length > 0 {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", start, start+length-1))
	}
	out, err := e.client.GetObject(context.TODO(), input)
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (e *S3Explorer) WriteFile(path string, r io.Reader, size int64) error {
	bucket, subpath, _, err := e.getBucketAndPath(path)
	if err != nil {
		return err
	}

	uploader := transfermanager.New(e.client, func(o *transfermanager.Options) {
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	input := &transfermanager.UploadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(subpath),
		Body:   r,
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	_, err = uploader.UploadObject(context.TODO(), input)
	return err
}

func (e *S3Explorer) Checksum(path string) (string, error) {
	bucket, subpath, _, err := e.getBucketAndPath(path)
	if err != nil {
		return "", err
	}

	if subpath == "" {
		return "", fmt.Errorf("cannot calculate checksum of bucket")
	}

	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(subpath),
	}
	head, err := e.client.HeadObject(context.TODO(), input)
	if err != nil {
		return "", err
	}

	if head.ETag != nil {
		return strings.Trim(*head.ETag, "\""), nil
	}
	return "", fmt.Errorf("no etag found")
}
