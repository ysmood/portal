package lib

import (
	"regexp"
	"sync"

	"github.com/ysmood/portal/lib/utils"
	"github.com/ysmood/umi"
)

type globCache struct {
	lock      *sync.Mutex
	descCache *umi.Cache
	ascCache  *umi.Cache
}

func (g *globCache) getCache(isDesc bool) *umi.Cache {
	if isDesc {
		return g.descCache
	}
	return g.ascCache
}

func (g *globCache) Get(isDesc bool, pattern string) (interface{}, bool) {
	return g.getCache(isDesc).Get(pattern)
}

func (g *globCache) Set(isDesc bool, pattern string, val interface{}) {
	g.getCache(isDesc).Set(pattern, val)
}

func (g *globCache) find(isDesc bool, uri string) (string, interface{}) {
	cache := g.getCache(isDesc)
	for _, pattern := range cache.Keys() {
		matched, err := regexp.MatchString(pattern, uri)

		if err == nil && matched {
			val, _ := cache.Get(pattern)
			return pattern, val
		}
	}

	return "", nil
}

func (g *globCache) AddToList(uri string) {
	pattern, list := g.find(true, uri)
	if list != nil {
		g.Set(true, pattern, append(list.([]interface{}), uri))
	}
	pattern, list = g.find(false, uri)
	if list != nil {
		g.Set(false, pattern, append(list.([]interface{}), uri))
	}
}

func (g *globCache) DelFromList(uri string) {
	pattern, list := g.find(true, uri)
	if list != nil {
		g.Set(true, pattern, utils.DelFromArr(list.([]interface{}), uri))
	}
	pattern, list = g.find(false, uri)
	if list != nil {
		g.Set(false, pattern, utils.DelFromArr(list.([]interface{}), uri))
	}
}
