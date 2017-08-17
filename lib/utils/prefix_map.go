package utils

import "strings"

// PrefixMap ...
type PrefixMap map[string]interface{}

// Get ...
func (p PrefixMap) Get(key string) (val interface{}, has bool) {
	for len(key) > 0 {
		val, has = p[key]

		if has {
			return
		}

		index := strings.LastIndexByte(key, '/')

		if index < 0 {
			return
		}

		key = key[0:index]
	}

	return
}
