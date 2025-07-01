package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
	"strconv"
	"encoding/base64"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"s3proxy/apigw"
	"s3proxy/backend"
	"s3proxy/logger"
)

// --- Структуры для XML-ответов (без изменений) ---
// ListBucketsResult представляет результат операции ListBuckets
type ListBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets Buckets  `xml:"Buckets"`
}

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type Buckets struct {
	Bucket []Bucket `xml:"Bucket"`
}

type Bucket struct {
	Name         string    `xml:"Name"`
	CreationDate time.Time `xml:"CreationDate"`
}

type ListObjectsV2Result struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string   `xml:"Name"`
	Prefix                string   `xml:"Prefix,omitempty"`
	KeyCount              int32    `xml:"KeyCount"`
	MaxKeys               int32    `xml:"MaxKeys"`
	IsTruncated           bool     `xml:"IsTruncated"`
	ContinuationToken     string   `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string   `xml:"NextContinuationToken,omitempty"`
	Contents              []Object `xml:"Contents"`
}

type Object struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
	StorageClass string    `xml:"StorageClass,omitempty"`
}


// --- Универсальный агрегатор для LIST-операций ---

type opResult[T any] struct {
	Backend *backend.Backend
	Result  T
	Error   error
}

// aggregateAndMerge - это STANDALONE (не метод!) функция-дженерик.
// Она принимает provider в качестве аргумента.
func aggregateAndMerge[T any](
	ctx context.Context,
	req *apigw.S3Request,
	backends []*backend.Backend,
	provider *backend.Manager, // <- ЗАВИСИМОСТЬ ПЕРЕДАЕТСЯ ЯВНО
	methodName string,
	performOp func(context.Context, *apigw.S3Request, *backend.Backend, string) opResult[T],
	mergeOp func(*apigw.S3Request, []opResult[T]) *apigw.S3Response,
) *apigw.S3Response {
	logger.Debug("aggregateAndMerge: Started for operation '%s' with %d backends.", methodName, len(backends))
	
	// 1. Декодирование токена пагинации
	backendTokens := make(map[string]string)
	if tokenStr := req.Query.Get("continuation-token"); tokenStr != "" {
		logger.Debug("aggregateAndMerge: Found continuation token, attempting to decode: %s", tokenStr)
		var proxyToken ProxyContinuationToken
		if data, err := base64.StdEncoding.DecodeString(tokenStr); err == nil {
			if json.Unmarshal(data, &proxyToken) == nil {
				backendTokens = proxyToken.BackendTokens
				logger.Debug("aggregateAndMerge: Successfully decoded tokens for backends: %v", backendTokens)
			} else {
				logger.Error("aggregateAndMerge: Failed to unmarshal JSON from continuation token.")
			}
		} else {
			logger.Error("aggregateAndMerge: Failed to decode base64 continuation token: %v", err)
		}
	}

	// 2. Параллельные запросы
	resultsChan := make(chan opResult[T], len(backends))
	var wg sync.WaitGroup
	for _, be := range backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			start := time.Now()
			
			logger.Debug("aggregateAndMerge: Starting '%s' for backend %s.", methodName, b.ID)
			result := performOp(ctx, req, b, backendTokens[b.ID])
			latency := time.Since(start)
			
			if result.Error == nil {
				logger.Debug("aggregateAndMerge: Success from backend %s for '%s' in %v.", b.ID, methodName, latency)
				provider.ReportSuccess(&backend.BackendResult{
					BackendID: b.ID, Method: methodName, StatusCode: http.StatusOK, Duration: latency,
				})
			} else {
				var apiErr smithy.APIError
				statusCode := http.StatusInternalServerError
				if errors.As(result.Error, &apiErr) {
					var httpErr interface{ HTTPStatusCode() int }
					if errors.As(apiErr, &httpErr) {
						statusCode = httpErr.HTTPStatusCode()
					}
				}
				logger.Error("aggregateAndMerge: Failure from backend %s for '%s' in %v. Status: %d, Error: %v", b.ID, methodName, latency, statusCode, result.Error)
				provider.ReportFailure(&backend.BackendResult{
					BackendID: b.ID, Method: methodName, StatusCode: statusCode, Err: result.Error, Duration: latency,
				})
			}
			resultsChan <- result
		}(be)
	}

	// 3. Сбор результатов
	go func() {
		wg.Wait()
		close(resultsChan)
		logger.Debug("aggregateAndMerge: All backend operations for '%s' are complete.", methodName)
	}()

	var allResults []opResult[T]
	for res := range resultsChan {
		allResults = append(allResults, res)
	}
	
	logger.Debug("aggregateAndMerge: Collected %d results. Proceeding to merge.", len(allResults))
	return mergeOp(req, allResults)
}

// --- Реализация ListObjectsV2 через универсальный агрегатор ---

// listObjects теперь просто вызывает standalone-функцию aggregateAndMerge
func (f *Fetcher) listObjects(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	return aggregateAndMerge(
		ctx, req, backends,
		f.backendProvider, // <- Передаем зависимость
		"LIST_OBJECTS",
		f.performListObjectsV2,
		f.mergeListObjectsV2Results,
	)
}

// performListObjectsV2 - это метод, который будет передан в aggregateAndMerge
func (f *Fetcher) performListObjectsV2(ctx context.Context, req *apigw.S3Request, b *backend.Backend, token string) opResult[*s3.ListObjectsV2Output] {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(b.Config.Bucket),
	}
	if p := req.Query.Get("prefix"); p != "" { input.Prefix = aws.String(p) }
	if d := req.Query.Get("delimiter"); d != "" { input.Delimiter = aws.String(d) }
	if t := token; t != "" { input.ContinuationToken = aws.String(t) }
	if maxKeysStr := req.Query.Get("max-keys"); maxKeysStr != "" {
		if maxKeys, err := strconv.ParseInt(maxKeysStr, 10, 32); err == nil && maxKeys > 0 {
			input.MaxKeys = aws.Int32(int32(maxKeys))
		}
	}
	
	logger.Debug(
		"performListObjectsV2: Sending request to backend %s: Bucket=%s, Prefix='%s', Delimiter='%s', Token='%s', MaxKeys=%d",
		b.ID,
		aws.ToString(input.Bucket),
		aws.ToString(input.Prefix),
		aws.ToString(input.Delimiter),
		aws.ToString(input.ContinuationToken),
		aws.ToInt32(input.MaxKeys),
	)

	result, err := b.S3Client.ListObjectsV2(ctx, input)

	// Добавляем логирование результата сразу после получения
	if err != nil {
		logger.Error("performListObjectsV2: Received error from backend %s: %v", b.ID, err)
	} else if result != nil {
		logger.Debug(
			"performListObjectsV2: Received response from backend %s: Keys=%d, IsTruncated=%v, NextToken='%s'",
			b.ID,
			len(result.Contents),
			aws.ToBool(result.IsTruncated),
			aws.ToString(result.NextContinuationToken),
		)
	}

	return opResult[*s3.ListObjectsV2Output]{Backend: b, Result: result, Error: err}
}

// mergeListObjectsV2Results - это метод, который также передается в aggregateAndMerge
func (f *Fetcher) mergeListObjectsV2Results(req *apigw.S3Request, results []opResult[*s3.ListObjectsV2Output]) *apigw.S3Response {
	objectsMap := make(map[string]Object)
	newBackendTokens := make(map[string]string)
	isTruncated := false

	for _, res := range results {
		if res.Error != nil || res.Result == nil {
			continue
		}
		for _, objSDK := range res.Result.Contents {
			key := aws.ToString(objSDK.Key)
			newObj := Object{
				Key:          key,
				LastModified: aws.ToTime(objSDK.LastModified),
				ETag:         aws.ToString(objSDK.ETag),
				Size:         aws.ToInt64(objSDK.Size),
				StorageClass: string(objSDK.StorageClass),
			}
			if existing, exists := objectsMap[key]; !exists || newObj.LastModified.After(existing.LastModified) {
				objectsMap[key] = newObj
			}
		}
		if aws.ToBool(res.Result.IsTruncated) {
			isTruncated = true
			if token := aws.ToString(res.Result.NextContinuationToken); token != "" {
				newBackendTokens[res.Backend.ID] = token
			}
		}
	}

	finalObjects := make([]Object, 0, len(objectsMap))
	for _, obj := range objectsMap {
		finalObjects = append(finalObjects, obj)
	}
	sort.Slice(finalObjects, func(i, j int) bool { return finalObjects[i].Key < finalObjects[j].Key })

	var nextTokenStr string
	if isTruncated && len(newBackendTokens) > 0 {
		proxyToken := ProxyContinuationToken{BackendTokens: newBackendTokens}
		if tokenBytes, err := json.Marshal(proxyToken); err == nil {
			nextTokenStr = base64.StdEncoding.EncodeToString(tokenBytes)
		}
	}
	
	maxKeys, _ := strconv.ParseInt(req.Query.Get("max-keys"), 10, 32)
	if maxKeys <= 0 { maxKeys = 1000 }
	
	finalResult := ListObjectsV2Result{
		Name:                  req.Bucket,
		Prefix:                req.Query.Get("prefix"),
		MaxKeys:               int32(maxKeys),
		KeyCount:              int32(len(finalObjects)),
		IsTruncated:           nextTokenStr != "", // Более надежная проверка
		ContinuationToken:     req.Query.Get("continuation-token"),
		NextContinuationToken: nextTokenStr,
		Contents:              finalObjects,
	}

	xmlData, err := xml.MarshalIndent(finalResult, "", "  ")
	if err != nil {
		return &apigw.S3Response{StatusCode: http.StatusInternalServerError, Error: err}
	}
	
	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", strconv.Itoa(len(xmlData)))

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(bytes.NewReader(xmlData)),
	}
}

// listBuckets выполняет операцию LIST BUCKETS
func (f *Fetcher) listBuckets(ctx context.Context, req *apigw.S3Request, backends []*backend.Backend) *apigw.S3Response {
	// Возвращаем один виртуальный бакет, указанный в конфигурации
	buckets := []Bucket{
		{
			Name:         f.virtualBucket,
			CreationDate: time.Now().UTC(), // Используем текущее время
		},
	}

	// Создаем XML ответ
	listResult := ListBucketsResult{
		Owner: Owner{
			ID:          "s3proxy-owner-id",
			DisplayName: "s3proxy-owner",
		},
		Buckets: Buckets{
			Bucket: buckets,
		},
	}

	xmlData, err := xml.Marshal(listResult)
	if err != nil {
		return &apigw.S3Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    make(http.Header),
			Error:      err,
		}
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(xmlData)))

	return &apigw.S3Response{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       io.NopCloser(bytes.NewReader(xmlData)),
	}
}


