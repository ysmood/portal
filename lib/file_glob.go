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

type matchInfo struct {
	pattern string
	order   bool
	list    interface{}
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

func (g *globCache) matches(uri string) (results []*matchInfo) {
	desc := g.getCache(true)
	asc := g.getCache(false)

	results = []*matchInfo{}

	for _, pattern := range desc.Keys() {
		matched, err := regexp.MatchString(pattern, uri)

		if err == nil && matched {
			val, _ := desc.Get(pattern)
			results = append(results, &matchInfo{
				pattern: pattern,
				order:   true,
				list:    val,
			})
		}
	}

	for _, pattern := range asc.Keys() {
		matched, err := regexp.MatchString(pattern, uri)

		if err == nil && matched {
			val, _ := asc.Get(pattern)
			results = append(results, &matchInfo{
				pattern: pattern,
				order:   false,
				list:    val,
			})
		}
	}
	return
}

func (g *globCache) UpdateToList(uri string) {
	matches := g.matches(uri)

	for _, match := range matches {
		if match.order {
			newList := []interface{}{uri}
			g.Set(true, match.pattern, utils.DelFromArr(match.list.([]interface{}), uri, newList))
		} else {
			g.Set(false, match.pattern, append(utils.DelFromArr(match.list.([]interface{}), uri, nil), uri))
		}
	}
}

func (g *globCache) DelFromList(uri string) {
	matches := g.matches(uri)
	for _, match := range matches {
		g.Set(true, match.pattern, utils.DelFromArr(match.list.([]interface{}), uri, nil))
	}
}
