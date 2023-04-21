package cache

import (
	"fmt"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
	"time"
)

type RedisCache struct {
	Config *config.Config
	Logger *logrus.Logger
	Client *redis.Client
}

func NewRedisCache(cfg *config.Config, logger *logrus.Logger) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.RedisHost, cfg.RedisPort),
		Password: "",
		DB:       0,
	})
	logger.Debug("connecting to redis...")
	_, err := client.Ping().Result()
	if err != nil {
		return nil, err
	}
	return &RedisCache{
		Config: cfg,
		Logger: logger,
		Client: client,
	}, nil
}

func (rc *RedisCache) Write(key, value string) error {
	rc.Logger.Debugf("redis write key: %s with value: %s", key, value)
	_, err := rc.Client.SAdd(key, value).Result()
	if err != nil {
		return err
	}
	return nil
}

func (rc *RedisCache) WriteTimer(key, value string, ttl time.Time) error {
	rc.Logger.Debugf("redis write ttl key %s:%s with ttl %+v", key, value, ttl)
	if !rc.TimerExists(key) {
		_, err := rc.Client.Set(key, value, 0).Result()
		if err != nil {
			return err
		}
		_, err = rc.Client.ExpireAt(key, ttl).Result()
		if err != nil {
			return err
		}
	}
	return nil
}

func (rc *RedisCache) TimerExists(key string) bool {
	exists, err := rc.Client.Exists(key).Result()
	if err != nil {
		return false
	}
	if exists == 1 {
		return true
	}
	return false
}

func (rc *RedisCache) Read(key string, value interface{}) error {
	rc.Logger.Debugf("redis read key: %s", key)
	return nil
}

func (rc *RedisCache) ReadOwners() ([]string, error) {
	rc.Logger.Debug("redis read bilge:owners")
	result, err := rc.Client.SMembers("bilge:owners").Result()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (rc *RedisCache) ReadCandidates(owner string) []string {
	rc.Logger.Debugf("redis read: bilge:candidates:%s", owner)
	answer := []string{}
	var cursor uint64
	iter := rc.Client.SScan(fmt.Sprintf("bilge:candidates:%s", owner), cursor, "", 10).Iterator()
	for iter.Next() {
		answer = append(answer, iter.Val())
	}
	if err := iter.Err(); err != nil {
		rc.Logger.Error(err)
		return answer
	}
	return answer
}

func (rc *RedisCache) CandidateExists(owner, candidate string) bool {
	rc.Logger.Debug("redis set check exists")
	result, err := rc.Client.SIsMember(fmt.Sprintf("bilge:candidates:%s", owner), candidate).Result()
	if err != nil {
		rc.Logger.Error(err)
		return false
	}
	return result
}

func (rc *RedisCache) Delete(key, value string) error {
	rc.Logger.Debugf("redis delete key: %s", key)
	_, err := rc.Client.SRem(key, value).Result()
	if err != nil {
		return err
	}
	return nil
}
