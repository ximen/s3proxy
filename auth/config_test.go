package auth

import (
	"testing"
)

func TestNewAuthenticatorFromConfig(t *testing.T) {
	t.Run("ValidStaticConfig", func(t *testing.T) {
		config := Config{
			Provider: "static",
			Static: &StaticConfig{
				Users: []UserConfig{
					{
						AccessKey:   "AKIAIOSFODNN7EXAMPLE",
						SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
						DisplayName: "test-user",
					},
				},
			},
		}

		auth, err := NewAuthenticatorFromConfig(&config)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if auth == nil {
			t.Error("Expected authenticator instance, got nil")
		}
	})

	t.Run("InvalidProvider", func(t *testing.T) {
		config := Config{
			Provider: "unknown",
		}

		auth, err := NewAuthenticatorFromConfig(&config)
		if err == nil {
			t.Error("Expected error for unknown provider")
		}
		if auth != nil {
			t.Error("Expected nil authenticator for invalid provider")
		}
	})

	t.Run("MissingStaticConfig", func(t *testing.T) {
		config := Config{
			Provider: "static",
			Static:   nil,
		}

		auth, err := NewAuthenticatorFromConfig(&config)
		if err == nil {
			t.Error("Expected error for missing static config")
		}
		if auth != nil {
			t.Error("Expected nil authenticator for invalid config")
		}
	})

	t.Run("EmptyUsers", func(t *testing.T) {
		config := Config{
			Provider: "static",
			Static: &StaticConfig{
				Users: []UserConfig{},
			},
		}

		auth, err := NewAuthenticatorFromConfig(&config)
		if err == nil {
			t.Error("Expected error for empty users list")
		}
		if auth != nil {
			t.Error("Expected nil authenticator for empty users")
		}
	})

	t.Run("MultipleUsers", func(t *testing.T) {
		config := Config{
			Provider: "static",
			Static: &StaticConfig{
				Users: []UserConfig{
					{
						AccessKey:   "AKIAIOSFODNN7EXAMPLE",
						SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
						DisplayName: "test-user-1",
					},
					{
						AccessKey:   "AKIAYDR45T3E2EXAMPLE",
						SecretKey:   "a82hdaHGTi92k/2kdldk29dGSH28skdEXAMPLEKEY",
						DisplayName: "test-user-2",
					},
				},
			},
		}

		auth, err := NewAuthenticatorFromConfig(&config)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if auth == nil {
			t.Error("Expected authenticator instance, got nil")
		}

		// Проверяем, что аутентификатор создан правильно
		staticAuth, ok := auth.(*StaticAuthenticator)
		if !ok {
			t.Error("Expected StaticAuthenticator instance")
		}
		if len(staticAuth.credentials) != 2 {
			t.Errorf("Expected 2 credentials, got %d", len(staticAuth.credentials))
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Provider != "static" {
		t.Errorf("Expected provider 'static', got '%s'", config.Provider)
	}

	if config.Static == nil {
		t.Error("Expected static config, got nil")
	}

	if len(config.Static.Users) == 0 {
		t.Error("Expected default users, got empty list")
	}

	// Проверяем, что можно создать аутентификатор из конфигурации по умолчанию
	auth, err := NewAuthenticatorFromConfig(config)
	if err != nil {
		t.Errorf("Expected no error creating authenticator from default config, got %v", err)
	}
	if auth == nil {
		t.Error("Expected authenticator instance from default config, got nil")
	}
}

func TestUserConfig(t *testing.T) {
	user := UserConfig{
		AccessKey:   "AKIAIOSFODNN7EXAMPLE",
		SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		DisplayName: "test-user",
	}

	if user.AccessKey == "" {
		t.Error("AccessKey should not be empty")
	}
	if user.SecretKey == "" {
		t.Error("SecretKey should not be empty")
	}
	if user.DisplayName == "" {
		t.Error("DisplayName should not be empty")
	}
}
