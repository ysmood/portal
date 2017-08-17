package lib

import (
	"encoding/json"
	"sync"
	"time"
)

const (
	reqStatusCodeActionTick  = 0
	reqStatusCodeActionClear = -1
)

type reqCount struct {
	statusCodeRWLock    sync.RWMutex
	qpsTimerSpan        time.Duration
	statusCodes         map[int]uint64
	chStatusCode        chan int
	statusCodeQPS       float64
	statusCodeLastTotal uint64
	statusCodeLastTime  time.Time
}

func newReqCount() *reqCount {
	return &reqCount{
		qpsTimerSpan: 100 * time.Millisecond,

		statusCodes:         map[int]uint64{},
		chStatusCode:        make(chan int, 100000),
		statusCodeQPS:       0,
		statusCodeLastTotal: 0,
		statusCodeLastTime:  time.Time{},
	}
}

func (rc *reqCount) loadStatusCode() {
	data, err := db.Get([]byte("reqStatusCodeCounts"), nil)

	if err != nil {
		return
	}

	if data == nil {
		return
	}

	json.Unmarshal(data, &rc.statusCodes)

	rc.statusCodeLastTime = time.Now()
	rc.statusCodeLastTotal = rc.statusCodeTotal()
}

func (rc *reqCount) statusCodeTotal() uint64 {
	sum := uint64(0)
	for k, v := range rc.statusCodes {
		if k == 600 {
			continue
		}
		sum += v
	}

	return sum
}

func (rc *reqCount) setStatusCodeQPS() {
	now := time.Now()
	span := now.Sub(rc.statusCodeLastTime)

	total := rc.statusCodeTotal()

	if span.Nanoseconds() >= int64(rc.qpsTimerSpan) {
		rc.statusCodeQPS = float64(total-rc.statusCodeLastTotal) / span.Seconds()
	}

	rc.statusCodeLastTime = now
	rc.statusCodeLastTotal = total
}

func (rc *reqCount) saveStatusCodeCounts() {
	data, _ := json.Marshal(rc.statusCodes)

	db.Put(
		[]byte("reqStatusCodeCounts"),
		data,
		nil,
	)
}

func (rc *reqCount) worker() {
	rc.loadStatusCode()

	go rc.statusCodeWorker()

	for code := range rc.chStatusCode {
		if code == reqStatusCodeActionTick {
			rc.saveStatusCodeCounts()
			rc.setStatusCodeQPS()
		} else if code == reqStatusCodeActionClear {
			rc.statusCodes = map[int]uint64{}
		} else {
			rc.statusCodeRWLock.Lock()
			rc.statusCodes[code]++
			rc.statusCodeRWLock.Unlock()
		}
	}
}

func (rc *reqCount) statusCodeWorker() {
	for {
		time.Sleep(rc.qpsTimerSpan)

		rc.chStatusCode <- reqStatusCodeActionTick
	}
}

func (rc *reqCount) getStatusCodes() map[int]uint64 {
	m := map[int]uint64{}
	rc.statusCodeRWLock.RLock()
	for k, v := range rc.statusCodes {
		m[k] = v
	}
	rc.statusCodeRWLock.RUnlock()
	return m
}
