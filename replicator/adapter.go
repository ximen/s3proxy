package replicator

// import (
// 	"s3proxy/backend"
// )

// // BackendAdapter адаптирует backend.BackendProvider к локальному типу BackendProvider
// type BackendAdapter struct {
// 	provider backend.BackendProvider
// }

// // NewBackendAdapter создает новый адаптер
// func NewBackendAdapter(provider backend.BackendProvider) *BackendAdapter {
// 	return &BackendAdapter{provider: provider}
// }

// // GetLiveBackends возвращает живые бэкенды, адаптированные к локальному типу
// func (a *BackendAdapter) GetLiveBackends() []*Backend {
// 	liveBackends := a.provider.GetLiveBackends()
// 	adapted := make([]*Backend, len(liveBackends))

// 	for i, b := range liveBackends {
// 		adapted[i] = &Backend{
// 			ID:                 b.ID,
// 			S3Client:           b.S3Client,
// 			Config:             b.Config,
// 			StreamingPutClient: b.StreamingPutClient,
// 		}
// 	}

// 	return adapted
// }

// // ReportSuccess передает отчет об успехе в оригинальный provider
// func (a *BackendAdapter) ReportSuccess(backendID string) {
// 	a.provider.ReportSuccess(backendID)
// }

// // ReportFailure передает отчет об ошибке в оригинальный provider
// func (a *BackendAdapter) ReportFailure(backendID string, err error) {
// 	a.provider.ReportFailure(backendID, err)
// }
