package replicator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"s3proxy/logger"
)

// MultipartStore управляет маппингами multipart upload
type MultipartStore struct {
	mu       sync.RWMutex
	mappings map[string]*multipartUploadMapping
	config   *Config
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewMultipartStore создает новое хранилище multipart маппингов
func NewMultipartStore(config *Config) *MultipartStore {
	if config == nil {
		config = DefaultConfig()
	}
	
	store := &MultipartStore{
		mappings: make(map[string]*multipartUploadMapping),
		config:   config,
		stopChan: make(chan struct{}),
	}
	
	// Запускаем фоновую очистку
	store.startCleanup()
	
	return store
}

// CreateMapping создает новый маппинг для multipart upload
func (ms *MultipartStore) CreateMapping(bucket, key string, backendUploads map[string]string) (string, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// Генерируем уникальный ProxyUploadID
	proxyUploadID, err := ms.generateUploadID()
	if err != nil {
		return "", fmt.Errorf("failed to generate upload ID: %w", err)
	}
	
	// Создаем маппинг
	mapping := &multipartUploadMapping{
		ProxyUploadID:  proxyUploadID,
		BackendUploads: backendUploads,
		CreatedAt:      time.Now(),
		Bucket:         bucket,
		Key:            key,
	}
	
	ms.mappings[proxyUploadID] = mapping
	
	logger.Debug("Created multipart mapping: proxy=%s, backends=%v", proxyUploadID, backendUploads)
	return proxyUploadID, nil
}

// GetMapping получает маппинг по ProxyUploadID
func (ms *MultipartStore) GetMapping(proxyUploadID string) (*multipartUploadMapping, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	mapping, exists := ms.mappings[proxyUploadID]
	if !exists {
		return nil, false
	}
	
	// Проверяем, не истек ли TTL
	if time.Since(mapping.CreatedAt) > ms.config.MultipartUploadTTL {
		logger.Debug("Multipart mapping expired: %s", proxyUploadID)
		return nil, false
	}
	
	return mapping, true
}

// DeleteMapping удаляет маппинг
func (ms *MultipartStore) DeleteMapping(proxyUploadID string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	delete(ms.mappings, proxyUploadID)
	logger.Debug("Deleted multipart mapping: %s", proxyUploadID)
}

// ListMappings возвращает все активные маппинги
func (ms *MultipartStore) ListMappings() []*multipartUploadMapping {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	now := time.Now()
	var activeMappings []*multipartUploadMapping
	
	for _, mapping := range ms.mappings {
		if now.Sub(mapping.CreatedAt) <= ms.config.MultipartUploadTTL {
			activeMappings = append(activeMappings, mapping)
		}
	}
	
	return activeMappings
}

// Stop останавливает фоновые процессы
func (ms *MultipartStore) Stop() {
	close(ms.stopChan)
	ms.wg.Wait()
	logger.Debug("Multipart store stopped")
}

// generateUploadID генерирует уникальный ID для multipart upload
func (ms *MultipartStore) generateUploadID() (string, error) {
	// Генерируем 16 случайных байт
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	
	// Преобразуем в hex строку с префиксом
	return "proxy-" + hex.EncodeToString(bytes), nil
}

// startCleanup запускает фоновую очистку устаревших маппингов
func (ms *MultipartStore) startCleanup() {
	ms.wg.Add(1)
	go func() {
		defer ms.wg.Done()
		
		ticker := time.NewTicker(ms.config.CleanupInterval)
		defer ticker.Stop()
		
		logger.Debug("Started multipart store cleanup with interval %v", ms.config.CleanupInterval)
		
		for {
			select {
			case <-ticker.C:
				ms.cleanup()
			case <-ms.stopChan:
				logger.Debug("Multipart store cleanup stopped")
				return
			}
		}
	}()
}

// cleanup удаляет устаревшие маппинги
func (ms *MultipartStore) cleanup() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	now := time.Now()
	var expiredKeys []string
	
	for key, mapping := range ms.mappings {
		if now.Sub(mapping.CreatedAt) > ms.config.MultipartUploadTTL {
			expiredKeys = append(expiredKeys, key)
		}
	}
	
	for _, key := range expiredKeys {
		delete(ms.mappings, key)
	}
	
	if len(expiredKeys) > 0 {
		logger.Debug("Cleaned up %d expired multipart mappings", len(expiredKeys))
	}
}

// Stats возвращает статистику хранилища
func (ms *MultipartStore) Stats() (total, active int) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	now := time.Now()
	total = len(ms.mappings)
	
	for _, mapping := range ms.mappings {
		if now.Sub(mapping.CreatedAt) <= ms.config.MultipartUploadTTL {
			active++
		}
	}
	
	return total, active
}
