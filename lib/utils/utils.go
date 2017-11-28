package utils

import (
	"bytes"
	"compress/gzip"
	"hash/crc32"
	"mime"
	"os"
	"os/signal"
	"strconv"
	"strings"
)

// Wait ...
func Wait(clean func()) {
	finish := make(chan bool)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		// sig is a ^C, handle it
		<-c
		clean()
		finish <- true
	}()

	<-finish
}

// ETag ...
func ETag(data []byte) []byte {
	return []byte("W/\"" +
		strconv.FormatUint(uint64(crc32.ChecksumIEEE(data)), 36) +
		"\"")
}

// GetURIPath ...
// return uri without query and the query
func GetURIPath(uri string) (string, string) {
	queryIndex := strings.IndexAny(uri, "?")

	if queryIndex > -1 {
		return uri[0:queryIndex], uri[queryIndex+1:]
	}

	return uri, ""
}

type version struct {
	major string
	minor string
	patch string
}

// CompareVersion ...
func CompareVersion(s1, s2 string) int {
	v1 := parseVersion(s1)
	v2 := parseVersion(s2)

	var res int
	res = CompareVersionSection(v1.major, v2.major)

	if res != 0 {
		return res
	}

	res = CompareVersionSection(v1.minor, v2.minor)

	if res != 0 {
		return res
	}

	return CompareVersionSection(v1.patch, v2.patch)
}

// CompareVersionSection ...
func CompareVersionSection(s1, s2 string) int {
	l1 := len(s1)
	l2 := len(s2)
	if l1 == l2 {
		return strings.Compare(s1, s2)
	}
	if l1 < l2 {
		return strings.Compare(strings.Repeat("0", l2-l1)+s1, s2)
	}
	return strings.Compare(s1, strings.Repeat("0", l1-l2)+s2)
}

func parseVersion(s string) *version {
	v := version{}
	sections := strings.Split(s, ".")
	l := len(sections)
	switch l {
	case 0:
		v.major = s
	case 1:
		v.major = sections[0]
	case 2:
		v.major = sections[0]
		v.minor = sections[1]
	default:
		v.major = sections[0]
		v.minor = sections[1]
		v.patch = sections[2]
	}
	return &v
}

// LookupStrEnv ...
func LookupStrEnv(key string, defaultVal string) string {
	s, has := os.LookupEnv(key)

	if has {
		return s
	}

	return defaultVal
}

// LookupIntEnv ...
func LookupIntEnv(key string, defaultVal int) int {
	s, has := os.LookupEnv(key)

	if has {
		i, err := strconv.ParseInt(s, 10, 32)
		if err == nil {
			return int(i)
		}
	}
	return defaultVal
}

// Slicer ...
func Slicer(left int, limit int, max int, maxLimit int) (int, int) {
	if left < 0 {
		left = 0
	}

	if limit < 0 {
		limit = 0
	}

	if limit > maxLimit {
		limit = maxLimit
	}

	right := left + limit

	if right > max {
		right = max
	}

	if left > right {
		left = right
	}

	return left, right
}

// DelFromArr ...
func DelFromArr(list []interface{}, target interface{}, newList []interface{}) []interface{} {
	if newList == nil {
		newList = []interface{}{}
	}

	for _, el := range list {
		if el == target {
			continue
		}
		newList = append(newList, el)
	}

	return newList
}

// Gzip compress data
func Gzip(data []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

// IsTextMIME ...
func IsTextMIME(contentType string) bool {
	t, _, _ := mime.ParseMediaType(contentType)
	return t == "application/json" ||
		t == "text/css" ||
		t == "text/html" ||
		t == "text/plain" ||
		t == "text/xml" ||
		t == "application/js"
}
