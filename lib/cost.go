package lib

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron"
	"github.com/ysmood/umi"
)

type costCache struct {
	lock         *sync.RWMutex
	chAdd        chan *costMessage
	cache        *umi.Cache
	tick         time.Time
	qpsTimerSpan time.Duration
}

type costMessage struct {
	uri  string
	cost uint64
}

type costInfo struct {
	cost       uint64
	count      uint64
	oldCount   uint64
	concurrent uint32
	qps        uint32
	rejected   uint64
}

var emptyCostInfo = &costInfo{
	cost:       0,
	count:      0,
	concurrent: 0,
	qps:        0,
	rejected:   0,
}

func newCostCache() *costCache {
	cost := &costCache{
		lock:  &sync.RWMutex{},
		chAdd: make(chan *costMessage, 100000),
		cache: umi.New(&umi.Options{
			MaxMemSize: 100 * 1024 * 1024, // 100MB
			GCSpan:     -1,
		}),
		tick:         time.Time{},
		qpsTimerSpan: 100 * time.Millisecond,
	}

	go func() {
		for msg := range cost.chAdd {
			cost.end(msg.uri, msg.cost)
		}
	}()

	go func() {
		for {
			time.Sleep(cost.qpsTimerSpan)

			cost.setQPS()
		}
	}()

	c := cron.New()

	// every day at 3am
	c.AddFunc("0 0 3 * * *", func() {
		cost.printList()

		cost.lock.Lock()
		cost.cache.Purge()
		fmt.Println("purge cost list:", time.Now())
		cost.lock.Unlock()
	})
	c.Start()

	return cost
}

func (c *costCache) setQPS() {
	now := time.Now()
	span := now.Sub(c.tick)

	c.lock.Lock()
	defer c.lock.Unlock()

	items := c.cache.Items()

	for _, item := range items {
		info := item.Value().(*costInfo)
		info.qps = uint32(float64(info.count-info.oldCount) / span.Seconds())
		info.oldCount = info.count
	}

	c.tick = now
}

func (c *costCache) printList() {
	c.lock.RLock()
	defer c.lock.RUnlock()

	items := c.cache.Items()

	type listItem struct {
		URI        string
		Cost       uint64
		Count      uint64
		Concurrent uint32
		QPS        uint32
	}

	list := []listItem{}

	for _, item := range items {
		info := item.Value().(*costInfo)
		list = append(list, listItem{
			URI:        item.Key(),
			Cost:       info.cost,
			Count:      info.count,
			Concurrent: info.concurrent,
			QPS:        info.qps,
		})
	}

	data, _ := json.Marshal(list)

	fmt.Println("*** cost log:", time.Now())
	fmt.Println(string(data))
}

func (c *costCache) end(uri string, num uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	cache, has := c.cache.Peek(uri)

	if !has {
		c.cache.Set(uri, &costInfo{
			cost:       num,
			count:      1,
			oldCount:   0,
			concurrent: 0,
			rejected:   0,
		})
		return
	}

	info := cache.(*costInfo)

	if info.concurrent > 0 {
		info.concurrent--
	}

	info.cost += num
}

func (c *costCache) many(uri string, quota uint64, concurrent uint32) bool {
	c.lock.Lock()
	defer c.lock.Unlock()

	cache, has := c.cache.Peek(uri)

	if !has {
		if concurrent < 1 {
			info := &costInfo{
				cost:       0,
				count:      0,
				oldCount:   0,
				concurrent: 0,
				rejected:   1,
			}
			c.cache.Set(uri, info)
			return true
		}
		info := &costInfo{
			cost:       0,
			count:      1,
			oldCount:   0,
			concurrent: 1,
			rejected:   0,
		}
		c.cache.Set(uri, info)
		return false
	}

	info := cache.(*costInfo)

	if info.concurrent >= concurrent || info.cost >= quota {
		info.rejected++
		return true
	}

	info.concurrent++
	info.count++

	return false
}

func (c *costCache) get(uri string) (info *costInfo) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	cache, has := c.cache.Peek(uri)

	if has {
		info = cache.(*costInfo)
	} else {
		info = emptyCostInfo
	}

	return
}
