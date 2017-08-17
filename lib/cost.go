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
	lock  *sync.RWMutex
	chAdd chan *costMessage
	cache *umi.Cache
}

type costMessage struct {
	uri  string
	cost uint64
}

func newCostCache() *costCache {
	cost := &costCache{
		lock:  &sync.RWMutex{},
		chAdd: make(chan *costMessage, 100000),
		cache: umi.New(&umi.Options{
			MaxMemSize: 100 * 1024 * 1024, // 100MB
			GCSpan:     -1,
		}),
	}

	go func() {
		for msg := range cost.chAdd {
			cost.add(msg.uri, msg.cost)
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
func (c *costCache) printList() {
	c.lock.RLock()
	defer c.lock.RUnlock()

	items := c.cache.Items()

	type listItem struct {
		URI  string
		Cost uint64
	}

	list := []listItem{}

	for _, item := range items {
		list = append(list, listItem{
			URI:  item.Key(),
			Cost: item.Value().(uint64),
		})
	}

	data, _ := json.Marshal(list)

	fmt.Println("*** cost log:", time.Now())
	fmt.Println(string(data))
}

func (c *costCache) add(uri string, num uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	old, has := c.cache.Peek(uri)
	if has {
		c.cache.Set(uri, old.(uint64)+num)
	} else {
		c.cache.Set(uri, num)
	}
}

func (c *costCache) get(uri string) uint64 {
	c.lock.RLock()
	defer c.lock.RUnlock()

	num, has := c.cache.Get(uri)

	if has {
		return num.(uint64)
	}

	return 0
}
