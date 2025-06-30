package auth

// Config содержит конфигурацию для модуля аутентификации
type Config struct {
	// Provider определяет тип провайдера аутентификации ("static", "vault", "iam", etc.)
	Provider string `yaml:"provider" json:"provider"`
	
	// Static содержит конфигурацию для StaticAuthenticator
	Static *StaticConfig `yaml:"static,omitempty" json:"static,omitempty"`
}

// StaticConfig содержит конфигурацию для статического аутентификатора
type StaticConfig struct {
	// Users содержит список пользователей и их ключей
	Users []UserConfig `yaml:"users" json:"users"`
}

// UserConfig содержит конфигурацию одного пользователя
type UserConfig struct {
	// AccessKey - публичный ключ доступа
	AccessKey string `yaml:"access_key" json:"access_key"`
	
	// SecretKey - секретный ключ
	SecretKey string `yaml:"secret_key" json:"secret_key"`
	
	// DisplayName - отображаемое имя пользователя
	DisplayName string `yaml:"display_name" json:"display_name"`
}

// NewAuthenticatorFromConfig создает аутентификатор на основе конфигурации
func NewAuthenticatorFromConfig(config *Config) (Authenticator, error) {
	switch config.Provider {
	case "static":
		if config.Static == nil {
			return nil, ErrInvalidAuthHeader // Можно создать специальную ошибку для конфигурации
		}
		
		// Проверяем, что есть пользователи
		if len(config.Static.Users) == 0 {
			return nil, ErrInvalidAuthHeader // Можно создать специальную ошибку для пустого списка пользователей
		}
		
		// Преобразуем конфигурацию в map для StaticAuthenticator
		credentials := make(map[string]SecretKey)
		for _, user := range config.Static.Users {
			credentials[user.AccessKey] = SecretKey{
				SecretAccessKey: user.SecretKey,
				DisplayName:     user.DisplayName,
			}
		}
		
		return NewStaticAuthenticator(credentials)
	default:
		return nil, ErrInvalidAuthHeader // Можно создать специальную ошибку для неизвестного провайдера
	}
}

// DefaultConfig возвращает конфигурацию по умолчанию с тестовыми пользователями
// func DefaultConfig() *Config {
// 	return &Config{
// 		Provider: "static",
// 		Static: &StaticConfig{
// 			Users: []UserConfig{
// 				{
// 					AccessKey:   "AKIAIOSFODNN7EXAMPLE",
// 					SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
// 					DisplayName: "test-user",
// 				},
// 				{
// 					AccessKey:   "AKIAYDR45T3E2EXAMPLE",
// 					SecretKey:   "a82hdaHGTi92k/2kdldk29dGSH28skdEXAMPLEKEY",
// 					DisplayName: "admin-user",
// 				},
// 			},
// 		},
// 	}
// }

// Validate проверяет корректность конфигурации аутентификации
func (c *Config) Validate() error {
	if c.Provider == "" {
		return ErrInvalidAuthHeader // Можно создать специальную ошибку
	}
	
	switch c.Provider {
	case "static":
		if c.Static == nil {
			return ErrInvalidAuthHeader
		}
		
		if len(c.Static.Users) == 0 {
			return ErrInvalidAuthHeader
		}
		
		// Проверяем каждого пользователя
		accessKeys := make(map[string]bool)
		for _, user := range c.Static.Users {
			if user.AccessKey == "" {
				return ErrInvalidAuthHeader
			}
			if user.SecretKey == "" {
				return ErrInvalidAuthHeader
			}
			
			// Проверяем уникальность access key
			if accessKeys[user.AccessKey] {
				return ErrInvalidAuthHeader // Дублирующийся access key
			}
			accessKeys[user.AccessKey] = true
		}
		
	default:
		return ErrInvalidAuthHeader // Неизвестный провайдер
	}
	
	return nil
}
