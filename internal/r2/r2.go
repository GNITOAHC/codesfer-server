package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Client struct {
	s3     *s3.Client
	bucket string
}

var R2Client Client

func Init(cfAccountID, cfAccessKey, cfSecretAccessKey, cfBucketName string) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfAccessKey, cfSecretAccessKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfAccountID))
	})

	R2Client = Client{
		s3:     client,
		bucket: cfBucketName,
	}
}

// Upload uploads a byte slice as an object to R2.
func (c *Client) Upload(ctx context.Context, key string, data []byte) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("upload object: %w", err)
	}
	return nil
}

// UploadStream uploads an io.Reader stream as an object to R2.
func (c *Client) UploadStream(ctx context.Context, key string, r io.Reader) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   r,
	})
	if err != nil {
		return fmt.Errorf("upload object (stream): %w", err)
	}
	return nil
}

// UploadMultipart streams a large file in parts.
func (c *Client) UploadMultipart(ctx context.Context, key string, r io.Reader, partSize int64) error {
	// 1. Initiate multipart upload
	createResp, err := c.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("create multipart upload: %w", err)
	}

	uploadID := *createResp.UploadId
	var completedParts []types.CompletedPart

	buf := make([]byte, partSize)
	partNum := int32(1)

	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			// Upload this part
			partResp, err := c.s3.UploadPart(ctx, &s3.UploadPartInput{
				Bucket:     aws.String(c.bucket),
				Key:        aws.String(key),
				UploadId:   aws.String(uploadID),
				PartNumber: aws.Int32(partNum), // new pointer each time
				Body:       bytes.NewReader(buf[:n]),
			})
			if err != nil {
				return fmt.Errorf("upload part %d: %w", partNum, err)
			}

			// Save ETag + part number
			completedParts = append(completedParts, types.CompletedPart{
				ETag:       partResp.ETag,
				PartNumber: aws.Int32(partNum), // must allocate fresh
			})

			partNum++
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read part: %w", readErr)
		}
	}

	// 3. Complete upload
	_, err = c.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}

	return nil
}

// Download retrieves an object from R2 and returns its contents as []byte.
func (c *Client) Download(ctx context.Context, key string) ([]byte, error) {
	resp, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("download object: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read object body: %w", err)
	}
	return data, nil
}

// GetObject returns the raw GetObjectOutput for streaming.
func (c *Client) GetObject(ctx context.Context, key string) (*s3.GetObjectOutput, error) {
	return c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
}
