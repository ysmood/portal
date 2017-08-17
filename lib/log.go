package lib

import (
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ysmood/umi"
)

type logCache struct {
	cache *umi.Cache
	index uint64
}

type httpLog struct {
	URI     string    `json:"uri"`
	Status  int       `json:"status"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

func (log *logCache) http(uri string, status int, msg string) {
	atomic.AddUint64(&log.index, 1)
	log.cache.Set(strconv.FormatUint(log.index, 36), &httpLog{
		URI:     uri,
		Status:  status,
		Message: msg,
		Time:    time.Now(),
	})
}
