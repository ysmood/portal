package utils

import (
	"fmt"
	"strings"
	"sync"
)

// PrefixMap ...
type PrefixMap struct {
	Dist map[string]interface{}
	Lock *sync.RWMutex
}

// Get ...
func (p PrefixMap) Get(key string) (val interface{}, has bool) {
	p.Lock.RLock()
	for len(key) > 0 {
		val, has = p.Dist[key]

		if has {
			p.Lock.RUnlock()
			return
		}

		index := strings.LastIndexByte(key, '/')

		if index < 0 {
			p.Lock.RUnlock()
			return
		}

		key = key[0:index]
	}
	p.Lock.RUnlock()
	return
}

// Set ...
func (p PrefixMap) Set(key string, value interface{}) {
	p.Lock.Lock()
	p.Dist[key] = value
	fmt.Println("update proxy rule:", key)
	p.Lock.Unlock()
}

// Del ...
func (p PrefixMap) Del(key string) {
	p.Lock.Lock()
	if _, has := p.Dist[key]; has {
		delete(p.Dist, key)
		fmt.Println("delete proxy rule:", key)
	}
	p.Lock.Unlock()
}
