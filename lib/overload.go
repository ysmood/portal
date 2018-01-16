package lib

import (
	"strconv"
	"sync"
	"time"

	"github.com/ysmood/umi"
)

type overloadMonitor struct {
	lock             *sync.Mutex
	action           chan *overloadMessage
	cache            *umi.Cache
	activityTimeSpan time.Duration
	fileHandler      overloadFileHandler
	globHandler      overloadGlobHandler
	popLimit         int
	tick             time.Time
}

const (
	overloadOriginFile = 0
	overloadOriginGisp = 1
)

type overloadFileHandler func(uri string)

type overloadGlobHandler func(uri string, desc bool)

type overloadMessage struct {
	uri    string
	origin int
	desc   bool
}

type overloadOptions struct {
	fileHandler overloadFileHandler
	globHandler overloadGlobHandler
}

func newOverloadMointer(option *overloadOptions) *overloadMonitor {
	monitor := &overloadMonitor{
		lock:   &sync.Mutex{},
		action: make(chan *overloadMessage),
		cache: umi.New(&umi.Options{
			MaxMemSize: 300 * 1024 * 1024, // 300MB
			GCSpan:     -1,
		}),
		activityTimeSpan: 1000 * time.Millisecond,
		fileHandler:      option.fileHandler,
		globHandler:      option.globHandler,
		popLimit:         5,
		tick:             time.Time{},
	}

	go func() {
		for msg := range monitor.action {
			switch msg.origin {
			case overloadOriginFile:
				key := strconv.Itoa(msg.origin) + msg.uri
				monitor.add(key, msg)
			case overloadOriginGisp:
				var key string
				if msg.desc {
					key = strconv.Itoa(msg.origin) + "1" + msg.uri
				} else {
					key = strconv.Itoa(msg.origin) + "0" + msg.uri
				}
				monitor.add(key, msg)
			}
		}
	}()

	go func() {
		for {
			time.Sleep(monitor.activityTimeSpan)

			monitor.pop()
		}
	}()

	return monitor
}

func (monitor *overloadMonitor) add(key string, msg *overloadMessage) {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	monitor.tick = time.Now()

	monitor.cache.Set(key, msg)
}

func (monitor *overloadMonitor) pop() {
	if monitor.cache.Count() == 0 {
		return
	}
	now := time.Now()
	if now.Sub(monitor.tick).Nanoseconds() < int64(monitor.activityTimeSpan) {
		return
	}
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	limit := monitor.popLimit
	items := monitor.cache.Items()
	count := len(items)

	if count > limit {
		items = items[count-limit : count]
	}

	for _, item := range items {
		msg := item.Value().(*overloadMessage)
		monitor.cache.Del(item.Key())
		switch msg.origin {
		case overloadOriginFile:
			monitor.fileHandler(msg.uri)
		case overloadOriginGisp:
			monitor.globHandler(msg.uri, msg.desc)
		}
	}
}

func (monitor *overloadMonitor) purge() {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	monitor.cache.Purge()
}
