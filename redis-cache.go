package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru"
	"github.com/mediocregopher/radix.v2/redis"
)

var client *redis.Client

type lockableCache struct {
	// lru.Cache is threadsafe but it is exported
	//  without a mutex.
	// Sometimes (as in expireRedisCache() method)
	//  need to lock the cache as a whole.
	lru  *lru.Cache
	lock *sync.RWMutex
}

var redisCache lockableCache

var expiryStop chan bool

var cacheHit int  // not threadsafe, purely for testing
var cacheMiss int // not threadsafe, purely for testing

func clearCacheStats() {

	cacheHit = 0
	cacheMiss = 0
}

type valueStruct struct {
	value      string
	expiryTime int64
}

func healthCheck(w http.ResponseWriter, req *http.Request) {

	res, err := client.Cmd("PING").Str()
	if err != nil {
		log.Fatal("Error on 'redis' connection: ", err)
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, res)
}

func getRedis(w http.ResponseWriter, req *http.Request) {

	params := mux.Vars(req)
	keyToGet := params["key"]
	cached, found := redisCache.lru.Get(keyToGet)
	if found {
		val := cached.(*valueStruct).value

		// Touch cache entry expiry timer
		redisCache.lock.Lock()
		redisCache.lru.Remove(keyToGet)
		entry := &valueStruct{val, time.Now().UnixNano()}
		redisCache.lru.Add(keyToGet, entry)
		redisCache.lock.Unlock()

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, val)
		cacheHit++
		return
	}
	cacheMiss++
	val, err := client.Cmd("GET", keyToGet).Str()
	if err == redis.ErrRespNil {
		w.WriteHeader(http.StatusNotFound)
		return
	} else if err != nil {
		log.Fatal("Error on 'redis' connection: ", err)
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, val)

	// Update caching
	entry := &valueStruct{val, time.Now().UnixNano()}
	redisCache.lru.Add(keyToGet, entry)
}

func createLockableCache(size int) lockableCache {

	lruCache, err := lru.New(size)
	if err != nil {
		log.Fatal("Could not create 'redis' cache, err: ", err)
	}
	return lockableCache{lru: lruCache, lock: new(sync.RWMutex)}
}

func startExpiryDaemon(timeout int, ms time.Duration) {

	expiryStop = make(chan bool)
	go func() {
		for {
			select {
			case <-expiryStop:
				return
			default:
				time.Sleep(ms * time.Millisecond)
				expireRedisCache(timeout)
			}
		}
	}()
}

func stopExpiryDaemon() {

	if expiryStop != nil {
		expiryStop <- true
	}
}

func expireRedisCache(ms int) {

	redisCache.lock.Lock()
	defer redisCache.lock.Unlock()

	expiryTimeLimit := time.Now().UnixNano() - int64(ms*1000000)
	//log.Printf("expireRedisCache expiryTimeLimit: %d timeLimit: %d\n", expiryTimeLimit, ms)

	// These are oldest to newest
	keys := redisCache.lru.Keys()
	for _, key := range keys {
		//log.Printf("expireRedisCache key: %s\n", key)
		cached, _ := redisCache.lru.Get(key)
		expiry := cached.(*valueStruct).expiryTime
		// Short-circuit if we no longer need to expire entries
		if expiry > expiryTimeLimit {
			break
		}
		//log.Printf("expireRedisCache - removing key: %s expiry: %d limit: %d diff: %d\n", key, expiry, expiryTimeLimit, (expiry - expiryTimeLimit))
		redisCache.lru.Remove(key)
	}
}

func createRedisClient(addr string) *redis.Client {

	client, err := redis.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Fatal("Error on 'redis' connection to '", addr, "' error: ", err)
	}
	return client
}

func createRouter() *mux.Router {

	router := mux.NewRouter()

	// Health Check
	router.HandleFunc("/ping", healthCheck).Methods("GET")

	// Redis GET
	router.HandleFunc("/{key}", getRedis).Methods("GET")

	return router
}

func getEnvironmentVariables() (redisAddr string, timeLimit int, cacheSize int, portStr string) {

	redisAddr = os.Getenv("REDIS")
	if redisAddr == "" {
		log.Printf("Invalid REDIS: '%s', setting to 'redis-backend:6379'\n", redisAddr)
		redisAddr = "redis-backend:6379"
	}

	var err error

	expiryTimeStr := os.Getenv("EXPIRY_TIME")
	timeLimit, err = strconv.Atoi(expiryTimeStr)
	if err != nil {
		log.Printf("Invalid EXPIRY_TIME: '%s', setting to 5 seconds\n", expiryTimeStr)
		timeLimit = 5000
	}

	cacheSizeStr := os.Getenv("CACHE_SIZE")
	cacheSize, err = strconv.Atoi(cacheSizeStr)
	if err != nil {
		log.Printf("Invalid CACHE_SIZE: '%s', setting to 100\n", portStr)
		cacheSize = 100
	}

	portStr = os.Getenv("PORT")
	_, err = strconv.Atoi(portStr)
	if err != nil {
		log.Printf("Invalid PORT: '%s', setting to 5000\n", portStr)
		portStr = "5000"
	}

	return
}

func main() {

	redisAddr, timeLimit, cacheSize, portStr := getEnvironmentVariables()
	log.Printf("Caching redis: %s, expiry=%d, cache size=%d, port=%s\n", redisAddr, timeLimit, cacheSize, portStr)

	redisCache = createLockableCache(cacheSize)

	startExpiryDaemon(timeLimit, 100)
	defer stopExpiryDaemon()

	client = createRedisClient(redisAddr)
	defer client.Close()

	router := createRouter()

	log.Printf("Caching redis proxy now listening ...\n")
	log.Fatal(http.ListenAndServe(":"+portStr, router))
}
