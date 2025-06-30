package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"s3proxy/apigw"
	"s3proxy/logger"
)

// MockHandler - тестовая реализация RequestHandler для демонстрации
type MockHandler struct{}

// NewMockHandler создает новый экземпляр тестового обработчика
func NewMockHandler() *MockHandler {
	return &MockHandler{}
}

// Handle реализует интерфейс RequestHandler
func (h *MockHandler) Handle(req *apigw.S3Request) *apigw.S3Response {
	logger.Debug("MockHandler: handling request - Operation: %s, Bucket: %s, Key: %s", 
		req.Operation.String(), req.Bucket, req.Key)
	
	switch req.Operation {
	case apigw.GetObject:
		return h.handleGetObject(req)
	case apigw.PutObject:
		return h.handlePutObject(req)
	case apigw.HeadObject:
		return h.handleHeadObject(req)
	case apigw.HeadBucket:
		return h.handleHeadBucket(req)
	case apigw.DeleteObject:
		return h.handleDeleteObject(req)
	case apigw.ListObjectsV2:
		return h.handleListObjects(req)
	case apigw.ListBuckets:
		return h.handleListBuckets(req)
	case apigw.CreateMultipartUpload:
		return h.handleCreateMultipartUpload(req)
	case apigw.UploadPart:
		return h.handleUploadPart(req)
	case apigw.CompleteMultipartUpload:
		return h.handleCompleteMultipartUpload(req)
	case apigw.AbortMultipartUpload:
		return h.handleAbortMultipartUpload(req)
	case apigw.ListMultipartUploads:
		return h.handleListMultipartUploads(req)
	default:
		return &apigw.S3Response{
			StatusCode: http.StatusNotImplemented,
			Error:      fmt.Errorf("operation %s not implemented", req.Operation.String()),
		}
	}
}

func (h *MockHandler) handleGetObject(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем получение объекта
	content := fmt.Sprintf("Mock content for object %s/%s", req.Bucket, req.Key)
	
	headers := make(http.Header)
	headers.Set("Content-Type", "text/plain")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(content)))
	headers.Set("ETag", `"mock-etag-12345"`)
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(content)),
	}
}

func (h *MockHandler) handlePutObject(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем загрузку объекта
	headers := make(http.Header)
	headers.Set("ETag", `"mock-etag-67890"`)
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (h *MockHandler) handleHeadObject(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем получение метаданных объекта
	headers := make(http.Header)
	headers.Set("Content-Type", "text/plain")
	headers.Set("Content-Length", "100")
	headers.Set("ETag", `"mock-etag-12345"`)
	headers.Set("Last-Modified", "Wed, 20 Jun 2025 20:00:00 GMT")
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (h *MockHandler) handleHeadBucket(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем проверку существования бакета
	headers := make(http.Header)
	headers.Set("x-amz-bucket-region", "us-east-1")
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (h *MockHandler) handleDeleteObject(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем удаление объекта
	return &apigw.S3Response{
		StatusCode: http.StatusNoContent,
	}
}

func (h *MockHandler) handleListObjects(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем список объектов
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Name>%s</Name>
    <Prefix></Prefix>
    <Marker></Marker>
    <MaxKeys>1000</MaxKeys>
    <IsTruncated>false</IsTruncated>
    <Contents>
        <Key>example-object.txt</Key>
        <LastModified>2025-06-20T20:00:00.000Z</LastModified>
        <ETag>"mock-etag-example"</ETag>
        <Size>100</Size>
        <StorageClass>STANDARD</StorageClass>
    </Contents>
</ListBucketResult>`, req.Bucket)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlContent)))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(xmlContent)),
	}
}

func (h *MockHandler) handleListBuckets(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем список бакетов
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Owner>
        <ID>mock-owner-id</ID>
        <DisplayName>mock-owner</DisplayName>
    </Owner>
    <Buckets>
        <Bucket>
            <Name>example-bucket</Name>
            <CreationDate>2025-06-20T20:00:00.000Z</CreationDate>
        </Bucket>
        <Bucket>
            <Name>another-bucket</Name>
            <CreationDate>2025-06-20T20:00:00.000Z</CreationDate>
        </Bucket>
    </Buckets>
</ListAllMyBucketsResult>`

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlContent)))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(xmlContent)),
	}
}

func (h *MockHandler) handleCreateMultipartUpload(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем создание multipart upload
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Bucket>%s</Bucket>
    <Key>%s</Key>
    <UploadId>mock-upload-id-12345</UploadId>
</InitiateMultipartUploadResult>`, req.Bucket, req.Key)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlContent)))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(xmlContent)),
	}
}

func (h *MockHandler) handleUploadPart(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем загрузку части
	headers := make(http.Header)
	headers.Set("ETag", `"mock-part-etag-12345"`)
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (h *MockHandler) handleCompleteMultipartUpload(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем завершение multipart upload
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Location>http://example.com/%s/%s</Location>
    <Bucket>%s</Bucket>
    <Key>%s</Key>
    <ETag>"mock-final-etag-12345"</ETag>
</CompleteMultipartUploadResult>`, req.Bucket, req.Key, req.Bucket, req.Key)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlContent)))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(xmlContent)),
	}
}

func (h *MockHandler) handleAbortMultipartUpload(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем отмену multipart upload
	return &apigw.S3Response{
		StatusCode: http.StatusNoContent,
	}
}

func (h *MockHandler) handleListMultipartUploads(req *apigw.S3Request) *apigw.S3Response {
	// Симулируем список multipart uploads
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Bucket>%s</Bucket>
    <KeyMarker></KeyMarker>
    <UploadIdMarker></UploadIdMarker>
    <NextKeyMarker></NextKeyMarker>
    <NextUploadIdMarker></NextUploadIdMarker>
    <MaxUploads>1000</MaxUploads>
    <IsTruncated>false</IsTruncated>
    <Upload>
        <Key>example-multipart-object</Key>
        <UploadId>mock-upload-id-12345</UploadId>
        <Initiated>2025-06-20T20:00:00.000Z</Initiated>
        <StorageClass>STANDARD</StorageClass>
    </Upload>
</ListMultipartUploadsResult>`, req.Bucket)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlContent)))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(xmlContent)),
	}
}
