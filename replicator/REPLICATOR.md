### Техническое Задание: Модуль Репликации (Replication Module)

#### 1. Назначение и Зона ответственности

Модуль **Репликации** (`ReplicationExecutor`) отвечает за выполнение операций, изменяющих состояние данных (`PUT`, `DELETE`, а также все шаги Multipart Upload), на одном или нескольких S3-бэкендах. Он получает запрос и политику от `Policy & Routing Engine` и организует параллельное выполнение запросов к бэкендам.

**Ключевые обязанности:**
*   **Реализация интерфейса `ReplicationExecutor`**: Предоставление методов `PutObject`, `DeleteObject`, `CreateMultipartUpload` и т.д.
*   **Получение списка бэкендов**: Перед каждой операцией запрашивать актуальный список "живых" бэкендов у `Backend Manager`.
*   **Параллельное исполнение**: Отправлять S3-запросы на все целевые бэкенды одновременно (в отдельных горутинах).
*   **Реализация политик подтверждения (`ack`)**:
    *   `ack=none`: Немедленно вернуть успех, а репликацию выполнять в фоне.
    *   `ack=one`: Вернуть успех, как только хотя бы один бэкенд ответил успешно. Остальные запросы продолжают выполняться в фоне.
    *   `ack=all`: Дождаться ответов от всех бэкендов и вернуть успех, только если все они завершились успешно.
*   **Потоковая передача данных**: Для операций `PUT` и `UploadPart` тело запроса должно передаваться на бэкенды потоком (`io.Reader`), без полной буферизации в памяти прокси.
*   **Агрегация ответов**: Сформировать единый `S3Response` на основе ответов от бэкендов (например, вернуть заголовки от первого успешного бэкенда).
*   **Обратная связь для Health Checker**: Сообщать `Backend Manager` об успехе или неудаче каждой операции с конкретным бэкендом (`ReportSuccess`/`ReportFailure`).
*   **Сбор детальных метрик**: Собирать и экспортировать метрики по каждому бэкенду и каждой операции.

**Вне зоны ответственности модуля:**
*   Принятие решений о том, *какую* политику использовать (это делает `Policy & Routing Engine`).
*   Аутентификация и авторизация входящего запроса.
*   Отслеживание состояния бэкендов (он только потребляет эту информацию от `Backend Manager`).

#### 2. Архитектура и компоненты

1.  **Replicator (Основной исполнитель)**: Структура, реализующая интерфейс `ReplicationExecutor` и содержащая зависимости.
2.  **Fan-Out/Fan-In Logic**: Ядро модуля. Для каждой операции создается "веер" из горутин (по одной на бэкенд), и затем собираются результаты в соответствии с политикой.
3.  **Request Cloner**: Так как тело запроса (`io.Reader`) можно прочитать только один раз, для отправки на несколько бэкендов его необходимо "размножить". Для этого используется `io.MultiWriter` в связке с `io.Pipe`.
4.  **Metrics Collector**: Встроенная логика для измерения времени выполнения операций и обновления Prometheus-метрик.

#### 3. Интерфейсы и структуры данных (Go)

Модуль реализует интерфейс, который мы определили ранее.

```go
// Напоминание интерфейса из Policy & Routing Engine
type ReplicationExecutor interface {
    PutObject(ctx context.Context, req *S3Request, policy WriteOperationPolicy) *S3Response
    DeleteObject(ctx context.Context, req *S3Request, policy WriteOperationPolicy) *S3Response
    // ... и другие методы для Multipart Upload
}

// Replicator - конкретная реализация ReplicationExecutor.
type Replicator struct {
    backendProvider BackendProvider // Зависимость: Backend Manager
    metrics         *monitoring.Metrics // Зависимость: Модуль метрик
    // ... логгер
}

// NewReplicator создает новый экземпляр модуля репликации.
func NewReplicator(provider BackendProvider, metrics *monitoring.Metrics) *Replicator {
    return &Replicator{
        backendProvider: provider,
        metrics:         metrics,
    }
}
```

#### 4. Детальная логика `PutObject`

Метод `PutObject` является самым сложным и показательным.

```go
func (r *Replicator) PutObject(ctx context.Context, req *S3Request, policy WriteOperationPolicy) *S3Response {
    // 1. Получаем "живые" бэкенды
    liveBackends := r.backendProvider.GetLiveBackends()
    if len(liveBackends) == 0 {
        return createErrorResponse(503, "ServiceUnavailable", "No available backends")
    }

    // 2. Политика ack=none (асинхронная)
    if policy.AckLevel == "none" {
        go r.performPutAsync(context.Background(), req, liveBackends) // Запускаем в фоне с новым контекстом
        return createSuccessResponse(req) // И немедленно возвращаем успех
    }
    
    // 3. Создаем каналы для сбора результатов
    resultsChan := make(chan *backendResult, len(liveBackends))
    
    // 4. Клонирование тела запроса для параллельной отправки
    // Тело req.Body нужно "размножить" на N потоков, где N = len(liveBackends)
    // Это делается с помощью io.Pipe. Один писатель, N читателей.
    bodyReaders := fanOutReader(req.Body, len(liveBackends))

    // 5. Запуск горутин для каждого бэкенда (Fan-Out)
    var wg sync.WaitGroup
    for i, backend := range liveBackends {
        wg.Add(1)
        go func(b *Backend, bodyReader io.Reader) {
            defer wg.Done()
            
            // Замер времени операции для конкретного бэкенда
            startTime := time.Now()
            
            // Создаем новый S3 PutObjectInput
            putInput := &s3.PutObjectInput{ ... } 
            
            // Выполняем запрос
            s3Resp, err := b.S3Client.PutObject(ctx, putInput)
            
            // Сообщаем о результате в Backend Manager
            if err != nil {
                r.backendProvider.ReportFailure(b.ID, err)
            } else {
                r.backendProvider.ReportSuccess(b.ID)
            }
            
            // Обновляем метрики
            latency := time.Since(startTime).Seconds()
            r.metrics.BackendRequestLatency.WithLabelValues(b.ID, "put").Observe(latency)
            // ... другие метрики

            // Отправляем результат в канал
            resultsChan <- &backendResult{response: s3Resp, err: err}

        }(backend, bodyReaders[i])
    }
    
    // Горутина для закрытия канала после завершения всех воркеров
    go func() {
        wg.Wait()
        close(resultsChan)
    }()

    // 6. Сбор результатов в соответствии с политикой (Fan-In)
    return r.aggregatePutResults(resultsChan, policy, len(liveBackends))
}

// aggregatePutResults реализует логику ack=one и ack=all
func (r *Replicator) aggregatePutResults(resultsChan <-chan *backendResult, policy WriteOperationPolicy, totalBackends int) *S3Response {
    successCount := 0
    errorCount := 0
    var firstSuccessResponse *S3Response // Сохраним ответ от первого успешного бэкенда
    
    for result := range resultsChan {
        if result.err == nil {
            successCount++
            if firstSuccessResponse == nil {
                firstSuccessResponse = convertToS3Response(result.response) // Преобразуем ответ в наш формат
            }
            // Для ack=one, возвращаем успех сразу после первого удачного ответа
            if policy.AckLevel == "one" {
                return firstSuccessResponse
            }
        } else {
            errorCount++
        }
    }
    
    // Логика для ack=all (проверяется после закрытия канала)
    if policy.AckLevel == "all" {
        if successCount == totalBackends {
            return firstSuccessResponse
        } else {
            return createErrorResponse(500, "InternalError", "Failed to replicate object to all backends")
        }
    }

    // Если мы дошли сюда при ack=one, значит, ни один бэкенд не ответил успехом
    if policy.AckLevel == "one" && successCount == 0 {
         return createErrorResponse(503, "ServiceUnavailable", "Failed to write to any backend")
    }

    return nil // Недостижимый код
}
```

#### 5. Поддержка Multipart Upload

Операции Multipart Upload требуют особого внимания, так как они состоят из нескольких шагов и включают состояние (`UploadId`).

*   **CreateMultipartUpload**: Должен быть выполнен на всех "живых" бэкендах. Прокси должен собрать все `UploadId` от бэкендов, сгенерировать свой собственный, уникальный `ProxyUploadId` и сохранить у себя маппинг `ProxyUploadId -> {backend1: uploadId1, backend2: uploadId2}`. Этот маппинг можно хранить в in-memory кэше с TTL. Клиенту возвращается `ProxyUploadId`.
*   **UploadPart**: Получив `ProxyUploadId`, прокси находит соответствующий маппинг и выполняет `UploadPart` на всех бэкендах, используя их родные `uploadId`.
*   **CompleteMultipartUpload**: Аналогично, прокси выполняет `Complete` на всех бэкендах. Это самая критическая фаза. Если на одном бэкенде `Complete` пройдет, а на другом нет, возникнет рассинхронизация. Политика `ack=all` здесь наиболее уместна.
*   **AbortMultipartUpload**: Должен быть выполнен на всех бэкендах для очистки.

#### 6. Метрики (Детализация)

Модуль метрик должен быть расширен для нужд репликации.

```go
type Metrics struct {
    // ... существующие метрики ...

    // Гистограмма времени выполнения запроса к конкретному бэкенду
    BackendRequestLatency *prometheus.HistogramVec
    
    // Счетчик успешных/неуспешных запросов к бэкенду
    BackendRequestsTotal *prometheus.CounterVec

    // Счетчик байт, записанных на каждый бэкенд
    BackendBytesWrittenTotal *prometheus.CounterVec
}

// Инициализация в NewMetrics()
...
    BackendRequestLatency: promauto.NewHistogramVec(
        prometheus.HistogramOpts{Name: "s3proxy_backend_request_latency_seconds", ...},
        []string{"backend_id", "operation"}, // Метки: wasabi-ams, put_object
    ),
    BackendRequestsTotal: promauto.NewCounterVec(
        prometheus.CounterOpts{Name: "s3proxy_backend_requests_total", ...},
        []string{"backend_id", "operation", "result"}, // Метки: wasabi-ams, put_object, success/error
    ),
...
```

**Где обновлять метрики:**
*   **Latency**: Замерять время выполнения S3-запроса в каждой горутине.
*   **Total**: Инкрементировать счетчик `success` или `error` после получения ответа.
*   **Bytes**: В горутине `PUT` можно обернуть `bodyReader` в специальный `io.Reader`, который будет считать проходящие через него байты и обновлять метрику.

#### 7. Ответственность разработчика

1.  Реализовать структуру `Replicator`, удовлетворяющую интерфейсу `ReplicationExecutor`.
2.  Реализовать метод `PutObject` с полной поддержкой параллельного выполнения и политик `ack`.
3.  Реализовать надежный механизм клонирования тела запроса (`io.Reader`).
4.  Реализовать методы `DeleteObject`, `CreateMultipartUpload`, `UploadPart`, `CompleteMultipartUpload`, `AbortMultipartUpload`, следуя той же логике параллельного выполнения.
5.  Разработать механизм хранения и управления маппингом `ProxyUploadId` для Multipart Upload.
6.  Интегрировать обратную связь с `Backend Manager` (`ReportSuccess`/`ReportFailure`) после каждого запроса к бэкенду.
7.  Интегрировать детальный сбор метрик (задержки, счетчики, объем данных) для каждой операции и каждого бэкенда.
8.  Написать unit-тесты, которые мокируют `BackendProvider` и S3-клиенты, чтобы проверить корректность работы политик `ack=none/one/all` и правильность агрегации результатов.