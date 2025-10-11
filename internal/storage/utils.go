package storage

import (
	"codeserver/internal/r2"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
)

func generateID(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range n {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b), nil
}

// r2path returns the path to object inside R2 bucket
// e.g. username: u, uid: 1234, path: dir/file.zip
// will return u/uid/dir/file.zip
func r2path(username, uid, path string) string {
	return fmt.Sprintf("%s/%s/%s", username, uid, strings.Trim(path, "/"))
}

// opupload will upload a file to R2 and insert a record to database
func opupload(ctx context.Context, file io.Reader, size int64, key, username, password, path string) (string, error) {
	const multipartThreshold = 100 << 20 // 100 MB

	if key == "" {
		uid, err := generateID(10)
		if err != nil {
			return "", errors.New("[op upload] [generate uid] generate uid failed: " + err.Error())
		}
		key = uid
	}

	objectPath := r2path(username, key, path)

	err := insert(key, username, path, password, objectPath)
	if err != nil {
		return "", errors.New("[op upload] [insert] insert failed: " + err.Error())
	}

	// return key, nil // For testing

	// Only upload after insert is successfull
	if size > multipartThreshold {
		log.Print("Stream via multipart")
		if err := r2.R2Client.UploadMultipart(ctx, objectPath, file, 8<<20); err != nil {
			return "", errors.New("[op upload] [multipart] multipart upload failed: " + err.Error())
		}
	} else {
		log.Print("Single PutObject")
		if err := r2.R2Client.UploadStream(ctx, objectPath, file); err != nil {
			return "", errors.New("[op upload] [single putobject] upload failed: " + err.Error())
		}
	}

	return key, nil
}
