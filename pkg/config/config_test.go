package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func GetNewValidConfig() Config {
	return Config{
		RedisHost: "127.0.0.1",
		RedisPort: uint32(5555),
	}
}

func TestSetDefaults(t *testing.T) {
	testCases := map[string]struct {
		config   *Config
		expected *Config
	}{
		"set config": {
			config: &Config{
				RedisHost: "localhost",
				RedisPort: uint32(5555),
			},
			expected: &Config{
				RedisHost: "localhost",
				RedisPort: uint32(5555),
			},
		},
		"set nothing": {
			config: &Config{},
			expected: &Config{
				RedisHost: DEFAULT_REDIS_HOST,
				RedisPort: DEFAULT_REDIS_PORT,
			},
		},
	}

	for desc, tc := range testCases {
		t.Run(desc, func(t *testing.T) {
			tc.config.setDefaults()
			assert.Equal(t, tc.expected, tc.config)
		})
	}
}

func TestValidate(t *testing.T) {
	testCases := map[string]struct {
		config    func(c Config) *Config
		expectErr bool
	}{
		"valid": {
			config: func(c Config) *Config {
				return &c
			},
			expectErr: false,
		},
	}

	for desc, tc := range testCases {
		t.Run(desc, func(t *testing.T) {
			actual := tc.config(GetNewValidConfig()).validate()
			isErr := actual != nil
			assert.Equal(t, tc.expectErr, isErr)
		})
	}
}
