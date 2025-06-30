package fetch

import "s3proxy/apigw"

// StubCache - это заглушка, реализующая интерфейс Cache.
// Она всегда сообщает, что в кэше ничего нет.
type StubCache struct{}

// NewStubCache создает новую заглушку.
func NewStubCache() *StubCache {
	return &StubCache{}
}

// Get для заглушки всегда возвращает "не найдено".
func (s *StubCache) Get(bucket, key string) (*apigw.S3Response, bool) {
	return nil, false
}
