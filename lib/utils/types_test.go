package utils_test

import (
	"testing"

	"github.com/ysmood/portal/lib/utils"
	"github.com/stretchr/testify/assert"
)

func TestPrefixMapGet(t *testing.T) {
	m := utils.PrefixMap{}

	m["http://a.com/a/b"] = 1
	m["http://a.com/c/d"] = 1
	m["http://a.com/e/f"] = 1

	_, has := m.Get("http://a.com/a/b/d/e")
	assert.Equal(t, true, has)

	_, has = m.Get("http://a.com/a/x/d/e")
	assert.Equal(t, false, has)
}
