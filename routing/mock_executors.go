package routing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"s3proxy/apigw"
	"s3proxy/logger"
)

// MockReplicationExecutor - mock реализация ReplicationExecutor для тестирования
type MockReplicationExecutor struct{}

// NewMockReplicationExecutor создает новый mock replication executor
func NewMockReplicationExecutor() *MockReplicationExecutor {
	return &MockReplicationExecutor{}
}

func (m *MockReplicationExecutor) PutObject(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("MockReplicationExecutor.PutObject called with policy: %+v", policy)
	logger.Info("Mock Replication: PUT %s/%s (ack=%s)", req.Bucket, req.Key, policy.AckLevel)
	
	headers := make(http.Header)
	headers.Set("ETag", `"mock-etag-12345"`)
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (m *MockReplicationExecutor) DeleteObject(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("MockReplicationExecutor.DeleteObject called with policy: %+v", policy)
	logger.Info("Mock Replication: DELETE %s/%s (ack=%s)", req.Bucket, req.Key, policy.AckLevel)
	
	return &apigw.S3Response{
		StatusCode: http.StatusNoContent,
	}
}

func (m *MockReplicationExecutor) CreateMultipartUpload(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("MockReplicationExecutor.CreateMultipartUpload called with policy: %+v", policy)
	logger.Info("Mock Replication: CREATE MULTIPART %s/%s (ack=%s)", req.Bucket, req.Key, policy.AckLevel)
	
	uploadId := "mock-upload-id-12345"
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Bucket>%s</Bucket>
    <Key>%s</Key>
    <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, req.Bucket, req.Key, uploadId)

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlContent)))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(xmlContent)),
	}
}

func (m *MockReplicationExecutor) UploadPart(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("MockReplicationExecutor.UploadPart called with policy: %+v", policy)
	partNumber := req.Query.Get("partNumber")
	uploadId := req.Query.Get("uploadId")
	logger.Info("Mock Replication: UPLOAD PART %s/%s part=%s uploadId=%s (ack=%s)", 
		req.Bucket, req.Key, partNumber, uploadId, policy.AckLevel)
	
	headers := make(http.Header)
	headers.Set("ETag", fmt.Sprintf(`"mock-part-etag-%s"`, partNumber))
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (m *MockReplicationExecutor) CompleteMultipartUpload(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("MockReplicationExecutor.CompleteMultipartUpload called with policy: %+v", policy)
	uploadId := req.Query.Get("uploadId")
	logger.Info("Mock Replication: COMPLETE MULTIPART %s/%s uploadId=%s (ack=%s)", 
		req.Bucket, req.Key, uploadId, policy.AckLevel)
	
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Location>http://s3proxy/%s/%s</Location>
    <Bucket>%s</Bucket>
    <Key>%s</Key>
    <ETag>"mock-complete-etag-12345"</ETag>
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

func (m *MockReplicationExecutor) AbortMultipartUpload(ctx context.Context, req *apigw.S3Request, policy WriteOperationPolicy) *apigw.S3Response {
	logger.Debug("MockReplicationExecutor.AbortMultipartUpload called with policy: %+v", policy)
	uploadId := req.Query.Get("uploadId")
	logger.Info("Mock Replication: ABORT MULTIPART %s/%s uploadId=%s (ack=%s)", 
		req.Bucket, req.Key, uploadId, policy.AckLevel)
	
	return &apigw.S3Response{
		StatusCode: http.StatusNoContent,
	}
}

// MockFetchingExecutor - mock реализация FetchingExecutor для тестирования
type MockFetchingExecutor struct{}

// NewMockFetchingExecutor создает новый mock fetching executor
func NewMockFetchingExecutor() *MockFetchingExecutor {
	return &MockFetchingExecutor{}
}

func (m *MockFetchingExecutor) GetObject(ctx context.Context, req *apigw.S3Request, policy ReadOperationPolicy) *apigw.S3Response {
	logger.Debug("MockFetchingExecutor.GetObject called with policy: %+v", policy)
	logger.Info("Mock Fetching: GET %s/%s (strategy=%s)", req.Bucket, req.Key, policy.Strategy)
	
	content := fmt.Sprintf("Mock content for %s/%s", req.Bucket, req.Key)
	
	headers := make(http.Header)
	headers.Set("Content-Type", "text/plain")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(content)))
	headers.Set("ETag", `"mock-etag-12345"`)
	headers.Set("Last-Modified", "Wed, 21 Jun 2025 15:00:00 GMT")
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(strings.NewReader(content)),
	}
}

func (m *MockFetchingExecutor) HeadObject(ctx context.Context, req *apigw.S3Request, policy ReadOperationPolicy) *apigw.S3Response {
	logger.Debug("MockFetchingExecutor.HeadObject called with policy: %+v", policy)
	logger.Info("Mock Fetching: HEAD %s/%s (strategy=%s)", req.Bucket, req.Key, policy.Strategy)
	
	headers := make(http.Header)
	headers.Set("Content-Type", "text/plain")
	headers.Set("Content-Length", "100")
	headers.Set("ETag", `"mock-etag-12345"`)
	headers.Set("Last-Modified", "Wed, 21 Jun 2025 15:00:00 GMT")
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (m *MockFetchingExecutor) HeadBucket(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	logger.Debug("MockFetchingExecutor.HeadBucket called")
	logger.Info("Mock Fetching: HEAD BUCKET %s", req.Bucket)
	
	headers := make(http.Header)
	headers.Set("x-amz-bucket-region", "us-east-1")
	
	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
	}
}

func (m *MockFetchingExecutor) ListObjects(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	logger.Debug("MockFetchingExecutor.ListObjects called")
	logger.Info("Mock Fetching: LIST OBJECTS %s", req.Bucket)
	
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Name>%s</Name>
    <Prefix></Prefix>
    <Marker></Marker>
    <MaxKeys>1000</MaxKeys>
    <IsTruncated>false</IsTruncated>
    <Contents>
        <Key>mock-object.txt</Key>
        <LastModified>2025-06-21T15:00:00.000Z</LastModified>
        <ETag>"mock-etag-12345"</ETag>
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

func (m *MockFetchingExecutor) ListBuckets(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	logger.Debug("MockFetchingExecutor.ListBuckets called")
	logger.Info("Mock Fetching: LIST BUCKETS")
	
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Owner>
        <ID>mock-owner-id</ID>
        <DisplayName>Mock Owner</DisplayName>
    </Owner>
    <Buckets>
        <Bucket>
            <Name>mock-bucket-1</Name>
            <CreationDate>2025-06-21T15:00:00.000Z</CreationDate>
        </Bucket>
        <Bucket>
            <Name>mock-bucket-2</Name>
            <CreationDate>2025-06-21T15:00:00.000Z</CreationDate>
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

func (m *MockFetchingExecutor) ListMultipartUploads(ctx context.Context, req *apigw.S3Request) *apigw.S3Response {
	logger.Debug("MockFetchingExecutor.ListMultipartUploads called")
	logger.Info("Mock Fetching: LIST MULTIPART UPLOADS %s", req.Bucket)
	
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
        <Key>mock-multipart-object.txt</Key>
        <UploadId>mock-upload-id-12345</UploadId>
        <Initiator>
            <ID>mock-initiator-id</ID>
            <DisplayName>Mock Initiator</DisplayName>
        </Initiator>
        <Owner>
            <ID>mock-owner-id</ID>
            <DisplayName>Mock Owner</DisplayName>
        </Owner>
        <StorageClass>STANDARD</StorageClass>
        <Initiated>2025-06-21T15:00:00.000Z</Initiated>
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
