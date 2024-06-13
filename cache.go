package main

import (
	"reflect"
	"sync"
)

type cacheMapType map[string]interface{}
type Cache struct {
	cacheMap cacheMapType
	cacheMutex sync.Mutex
}

var wsCache Cache = Cache{ cacheMap: make(cacheMapType) }
var mqttCache Cache = Cache{ cacheMap: make(cacheMapType) }

func (c *Cache) update(name string, data interface{}) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	old := c.cacheMap[name]
	if !reflect.DeepEqual(old, data) {
		Dispatcher.broadcastEvent(name, data)
		c.cacheMap[name] = data
	}
}

func (c *Cache) get(name string) interface{} {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	return c.cacheMap[name]
}

func (c *Cache) clear() {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	c.cacheMap = make(cacheMapType)
}

func (c *Cache) dump() cacheMapType {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	n := make(cacheMapType)
	for k, v := range c.cacheMap {
		n[k] = v
	}
	return n
}
