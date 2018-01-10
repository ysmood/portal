package lib

import (
	"regexp"
	"time"

	"github.com/ysmood/umi"
)

type runtimeCache struct {
	cache *umi.Cache
}

type runtimeInfo struct {
	uri   string
	value interface{}
	deps  []string
}

// Un thread safety
func newRuntimeCache() *runtimeCache {
	rt := &runtimeCache{
		cache: umi.New(&umi.Options{
			MaxMemSize:  200 * 1024 * 1024, // 200MB
			PromoteRate: -1,
			TTL:         10 * time.Minute,
		}),
	}

	return rt
}

func (rt *runtimeCache) set(uri string, key string, value interface{}, deps []string) {
	newKey := uri + " " + key
	rt.cache.Set(newKey, &runtimeInfo{
		uri:   uri,
		value: value,
		deps:  deps,
	})
}

func (rt *runtimeCache) get(uri string, key string) (interface{}, bool) {
	newKey := uri + " " + key
	value, has := rt.cache.Get(newKey)
	if has {
		return value.(*runtimeInfo).value, has
	}
	return nil, has
}

func (rt *runtimeCache) flush(uri string) {
	items := rt.cache.Items()
	for _, item := range items {
		info := *item.Value().(*runtimeInfo)
		for _, pattern := range info.deps {
			matched, err := regexp.MatchString(pattern, uri)
			if err == nil && matched || pattern == uri {
				rt.cache.Del(item.Key())
				break
			}
		}
	}
}

func (rt *runtimeCache) purge() {
	rt.cache.Purge()
}
