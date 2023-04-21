package cache

import (
	"time"
)

type Cache interface {
	Write(key, value string) error
	Read(key string, value interface{}) error
	ReadOwners() ([]string, error)
	ReadCandidates(owner string) []string
	CandidateExists(owner, candidate string) bool
	WriteTimer(key, value string, ttl time.Time) error
	TimerExists(key string) bool
	Delete(key, value string) error
}

type MockCache struct{}

func NewMockCache() *MockCache                                          { return &MockCache{} }
func (mc *MockCache) Write(key, value string) error                     { return nil }
func (mc *MockCache) Read(key string, value interface{}) error          { return nil }
func (mc *MockCache) ReadOwners() ([]string, error)                     { return nil, nil }
func (mc *MockCache) ReadCandidates(owner string) []string              { return nil }
func (mc *MockCache) CandidateExists(owner, candidate string) bool      { return false }
func (mc *MockCache) WriteTimer(key, value string, ttl time.Time) error { return nil }
func (mc *MockCache) TimerExists(key string) bool                       { return false }
func (mc *MockCache) Delete(key, value string) error                    { return nil }
