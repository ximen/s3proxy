package apigw

import (
	"net/http"
	"net/url"
	"testing"
)

func TestRequestParser_Parse(t *testing.T) {
	parser := NewRequestParser()

	tests := []struct {
		name           string
		method         string
		path           string
		query          string
		expectedOp     S3Operation
		expectedBucket string
		expectedKey    string
		expectError    bool
	}{
		// GET операции
		{
			name:           "GET object",
			method:         "GET",
			path:           "/my-bucket/path/to/object.txt",
			expectedOp:     GetObject,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},
		{
			name:           "List objects",
			method:         "GET",
			path:           "/my-bucket/",
			expectedOp:     ListObjectsV2,
			expectedBucket: "my-bucket",
			expectedKey:    "",
		},
		{
			name:           "List objects without trailing slash",
			method:         "GET",
			path:           "/my-bucket",
			expectedOp:     ListObjectsV2,
			expectedBucket: "my-bucket",
			expectedKey:    "",
		},
		{
			name:       "List buckets",
			method:     "GET",
			path:       "/",
			expectedOp: ListBuckets,
		},
		{
			name:           "List multipart uploads",
			method:         "GET",
			path:           "/my-bucket/",
			query:          "uploads",
			expectedOp:     ListMultipartUploads,
			expectedBucket: "my-bucket",
		},

		// PUT операции
		{
			name:           "PUT object",
			method:         "PUT",
			path:           "/my-bucket/path/to/object.txt",
			expectedOp:     PutObject,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},
		{
			name:           "Upload part",
			method:         "PUT",
			path:           "/my-bucket/path/to/object.txt",
			query:          "partNumber=1&uploadId=abc123",
			expectedOp:     UploadPart,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},

		// POST операции
		{
			name:           "Create multipart upload",
			method:         "POST",
			path:           "/my-bucket/path/to/object.txt",
			query:          "uploads",
			expectedOp:     CreateMultipartUpload,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},
		{
			name:           "Complete multipart upload",
			method:         "POST",
			path:           "/my-bucket/path/to/object.txt",
			query:          "uploadId=abc123",
			expectedOp:     CompleteMultipartUpload,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},

		// DELETE операции
		{
			name:           "DELETE object",
			method:         "DELETE",
			path:           "/my-bucket/path/to/object.txt",
			expectedOp:     DeleteObject,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},
		{
			name:           "Abort multipart upload",
			method:         "DELETE",
			path:           "/my-bucket/path/to/object.txt",
			query:          "uploadId=abc123",
			expectedOp:     AbortMultipartUpload,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},

		// HEAD операции
		{
			name:           "HEAD object",
			method:         "HEAD",
			path:           "/my-bucket/path/to/object.txt",
			expectedOp:     HeadObject,
			expectedBucket: "my-bucket",
			expectedKey:    "path/to/object.txt",
		},
		{
			name:           "HEAD bucket",
			method:         "HEAD",
			path:           "/my-bucket/",
			expectedOp:     HeadBucket,
			expectedBucket: "my-bucket",
			expectedKey:    "",
		},

		// Ошибочные случаи
		{
			name:        "Unsupported method",
			method:      "PATCH",
			path:        "/my-bucket/object.txt",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Создаем URL
			u, err := url.Parse("http://localhost:9000" + tt.path)
			if err != nil {
				t.Fatalf("Failed to parse URL: %v", err)
			}

			// Добавляем query параметры
			if tt.query != "" {
				u.RawQuery = tt.query
			}

			// Создаем HTTP запрос
			req := &http.Request{
				Method: tt.method,
				URL:    u,
				Header: make(http.Header),
				Body:   http.NoBody,
			}

			// Парсим запрос
			s3req, err := parser.Parse(req)

			// Проверяем ошибки
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Проверяем результаты
			if s3req.Operation != tt.expectedOp {
				t.Errorf("Expected operation %v, got %v", tt.expectedOp, s3req.Operation)
			}

			if s3req.Bucket != tt.expectedBucket {
				t.Errorf("Expected bucket %q, got %q", tt.expectedBucket, s3req.Bucket)
			}

			if s3req.Key != tt.expectedKey {
				t.Errorf("Expected key %q, got %q", tt.expectedKey, s3req.Key)
			}
		})
	}
}

func TestRequestParser_ParsePath(t *testing.T) {
	parser := NewRequestParser()

	tests := []struct {
		name           string
		path           string
		expectedBucket string
		expectedKey    string
	}{
		{
			name:           "Root path",
			path:           "/",
			expectedBucket: "",
			expectedKey:    "",
		},
		{
			name:           "Bucket only",
			path:           "/my-bucket",
			expectedBucket: "my-bucket",
			expectedKey:    "",
		},
		{
			name:           "Bucket with trailing slash",
			path:           "/my-bucket/",
			expectedBucket: "my-bucket",
			expectedKey:    "",
		},
		{
			name:           "Simple object",
			path:           "/my-bucket/object.txt",
			expectedBucket: "my-bucket",
			expectedKey:    "object.txt",
		},
		{
			name:           "Nested object",
			path:           "/my-bucket/folder/subfolder/object.txt",
			expectedBucket: "my-bucket",
			expectedKey:    "folder/subfolder/object.txt",
		},
		{
			name:           "Object with special characters",
			path:           "/my-bucket/path with spaces/object-name_123.txt",
			expectedBucket: "my-bucket",
			expectedKey:    "path with spaces/object-name_123.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3req := &S3Request{}
			err := parser.parsePath(tt.path, s3req)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if s3req.Bucket != tt.expectedBucket {
				t.Errorf("Expected bucket %q, got %q", tt.expectedBucket, s3req.Bucket)
			}

			if s3req.Key != tt.expectedKey {
				t.Errorf("Expected key %q, got %q", tt.expectedKey, s3req.Key)
			}
		})
	}
}

func TestS3Operation_String(t *testing.T) {
	tests := []struct {
		op       S3Operation
		expected string
	}{
		{PutObject, "PUT_OBJECT"},
		{GetObject, "GET_OBJECT"},
		{HeadObject, "HEAD_OBJECT"},
		{HeadBucket, "HEAD_BUCKET"},
		{DeleteObject, "DELETE_OBJECT"},
		{ListObjectsV2, "LIST_OBJECTS_V2"},
		{CreateMultipartUpload, "CREATE_MULTIPART_UPLOAD"},
		{UploadPart, "UPLOAD_PART"},
		{CompleteMultipartUpload, "COMPLETE_MULTIPART_UPLOAD"},
		{AbortMultipartUpload, "ABORT_MULTIPART_UPLOAD"},
		{ListMultipartUploads, "LIST_MULTIPART_UPLOADS"},
		{ListBuckets, "LIST_BUCKETS"},
		{UnsupportedOperation, "UNSUPPORTED_OPERATION"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.op.String()
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
