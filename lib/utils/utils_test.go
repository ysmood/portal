package utils_test

import (
	"testing"

	"fmt"

	"github.com/ysmood/portal/lib/utils"
	"github.com/stretchr/testify/assert"
)

func TestGetURIPath(t *testing.T) {
	var a, b string
	a, b = utils.GetURIPath("test?query")
	fmt.Println(a, b)
	a, b = utils.GetURIPath("test?")
	fmt.Println(a, b)
	a, b = utils.GetURIPath("test")
	fmt.Println(a, b)
}

func TestCompareVersion(t *testing.T) {
	assert.Equal(t, -1, utils.CompareVersion("1.1.1", "2.2.2"))
	assert.Equal(t, 1, utils.CompareVersion("3.1.1", "2.2.2"))
	assert.Equal(t, 0, utils.CompareVersion("1.2.3", "1.2.3"))
	assert.Equal(t, -1, utils.CompareVersion("1.2", "1.2.3"))
	assert.Equal(t, 1, utils.CompareVersion("1.2", "1"))
	assert.Equal(t, -1, utils.CompareVersion("2", "3"))
}
