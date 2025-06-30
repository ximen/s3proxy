package auth

import (
	"errors"
	"s3proxy/apigw"
)

// Authenticator - это универсальный интерфейс для всех модулей аутентификации.
type Authenticator interface {
	// Authenticate проверяет подлинность запроса.
	// На вход получает S3Request, так как для вычисления подписи SigV4
	// необходимы данные всего запроса (метод, URL, заголовки, хэш тела).
	// Возвращает подтвержденную личность пользователя или ошибку аутентификации.
	Authenticate(req *apigw.S3Request) (*UserIdentity, error)
}

// UserIdentity представляет подтвержденную личность пользователя.
// Эта структура будет передаваться дальше в модуль авторизации.
type UserIdentity struct {
	// Уникальный идентификатор пользователя, он же ключ доступа.
	AccessKey string

	// Отображаемое имя пользователя (опционально, для логов и UI).
	DisplayName string

	// Можно добавить другие поля для будущих нужд, например, список ролей.
	// Roles []string
}

// Пользовательские ошибки для точной диагностики
var (
	// ErrMissingAuthHeader - отсутствует заголовок Authorization.
	ErrMissingAuthHeader = errors.New("missing authorization header")
	// ErrInvalidAuthHeader - некорректный формат заголовка Authorization.
	ErrInvalidAuthHeader = errors.New("invalid authorization header")
	// ErrInvalidAccessKeyID - предоставленный Access Key не найден в системе.
	ErrInvalidAccessKeyID = errors.New("invalid access key ID")
	// ErrSignatureMismatch - вычисленная подпись не совпадает с предоставленной.
	ErrSignatureMismatch = errors.New("signature does not match")
	// ErrRequestExpired - временная метка запроса находится за пределами допустимого окна.
	ErrRequestExpired = errors.New("request has expired")
)

// SecretKey представляет секретный ключ и связанные с ним данные пользователя.
type SecretKey struct {
	SecretAccessKey string
	DisplayName     string
}
