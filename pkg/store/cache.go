package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
	"github.com/redis/go-redis/v9"
)

// Cache provides a Redis-backed cache for generated responses.
type Cache struct {
	client   *redis.Client
	ttl      time.Duration
	disabled atomic.Bool
	once     sync.Once
}

// NewCache creates a new Redis cache. Returns nil, nil if addr is empty.
func NewCache(addr string) (*Cache, error) {
	if addr == "" {
		return nil, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		log.Printf("WARNING: Redis unavailable at %s: %v — cache disabled", addr, err)
		c := &Cache{ttl: 7 * 24 * time.Hour}
		c.disabled.Store(true)
		return c, nil
	}

	return &Cache{
		client: client,
		ttl:    7 * 24 * time.Hour,
	}, nil
}

// GenerateKey builds a deterministic cache key from provider, model, input, and params.
func GenerateKey(provider, model, input string, params map[string]any) string {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte("|"))
	h.Write([]byte(model))
	h.Write([]byte("|"))
	h.Write([]byte(input))
	h.Write([]byte("|"))

	if len(params) > 0 {
		// Sort keys for determinism.
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, _ := json.Marshal(params[k])
			h.Write([]byte(k))
			h.Write([]byte("="))
			h.Write(v)
			h.Write([]byte(";"))
		}
	}

	return fmt.Sprintf("jj:gen:%x", h.Sum(nil))
}

// Get retrieves a cached response. Returns nil, nil on miss.
func (c *Cache) Get(ctx context.Context, key string) (*models.GenerateResponse, error) {
	if c == nil || c.disabled.Load() {
		return nil, nil
	}

	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		c.disableOnce(err)
		return nil, nil
	}

	var resp models.GenerateResponse
	if err := json.Unmarshal([]byte(val), &resp); err != nil {
		return nil, nil
	}
	return &resp, nil
}

// Set stores a response in the cache.
func (c *Cache) Set(ctx context.Context, key string, resp *models.GenerateResponse) error {
	if c == nil || c.disabled.Load() {
		return nil
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		c.disableOnce(err)
		return nil
	}
	return nil
}

// Close closes the Redis client connection.
func (c *Cache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *Cache) disableOnce(err error) {
	c.once.Do(func() {
		log.Printf("WARNING: Redis error, disabling cache: %v", err)
		c.disabled.Store(true)
	})
}
