package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru"
	"github.com/mediocregopher/radix.v2/redis"
)

var redisClient *redis.Client

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

	res, err := redisClient.Cmd("PING").Str()
	if err != nil {
		log.Fatal("healthCheck error: ", err)
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, res)
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

func createRedisClient(addr string) (*redis.Client, error) {

	return redis.DialTimeout("tcp", addr, 5*time.Second)
}

func createRouter() *mux.Router {

	router := mux.NewRouter()

	// Health Check
	router.HandleFunc("/ping", healthCheck).Methods("GET")

	// Redis GET
	router.HandleFunc("/{key}", getRedis).Methods("GET")

	return router
}

func getRedisValue(key string) (string, error) {

	cached, found := redisCache.lru.Get(key)
	if found {
		val := cached.(*valueStruct).value

		// Touch cache entry expiry timer
		redisCache.lock.Lock()
		redisCache.lru.Remove(key)
		entry := &valueStruct{val, time.Now().UnixNano()}
		redisCache.lru.Add(key, entry)
		redisCache.lock.Unlock()

		cacheHit++
		return val, nil
	}
	cacheMiss++
	val, err := redisClient.Cmd("GET", key).Str()
	if err == redis.ErrRespNil {
		return "", redis.ErrRespNil
	}
	if err != nil {
		log.Printf("getRedisValue for key '%s', error: %s\n", key, err)
		return "", err
	}

	// Update caching
	entry := &valueStruct{val, time.Now().UnixNano()}
	redisCache.lru.Add(key, entry)
	return val, nil
}

func startListener(portStr string) error {

	nlr, err := net.Listen("tcp", ":"+portStr)
	if err != nil {
		return err
	}
	defer nlr.Close()

	log.Printf("Caching TCP redis proxy now listening on port " + portStr + "...\n")

	for {
		conn, err := nlr.Accept()
		if err != nil {
			log.Println("startListener - error accepting 'tcp' connection:", err)
		}
		log.Println("Accepted conn:", conn)
		go handleRequest(conn)
	}
}

var redisGet = regexp.MustCompile(`^\*\d+\r\n\$\d+\r\nGET\r\n\$\d+\r\n.*\r\n`)

func handleRequest(conn net.Conn) {

	defer conn.Close()

	buf := make([]byte, 1024)

	length, err := conn.Read(buf)
	if err != nil {
		log.Println("Error reading:", err.Error())
	}

	if redisGet.Match(buf) {

		//log.Printf("Got redis request, length %d, '%s'\n", length, buf[:length])

		keyToGet := string(unwrapRedisKey(buf[:length]))
		val, _ := getRedisValue(keyToGet)
		conn.Write([]byte(wrapRedisValue(val)))
		return
	}

	log.Println("Got bad request: ", buf)
	log.Println("Got bad request: ", string(buf))
}

func getRedis(w http.ResponseWriter, req *http.Request) {

	//	log.Println("Got request", req)
	params := mux.Vars(req)
	keyToGet := params["key"]
	val, err := getRedisValue(keyToGet)
	if err == redis.ErrRespNil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, val)
}

func main() {

	redisAddr, timeLimit, cacheSize, portStr, portType := getEnvironmentVariables()
	log.Printf("Caching redis: %s, expiry=%d, cache size=%d, port=%s, type=%s\n", redisAddr, timeLimit, cacheSize, portStr, portType)

	redisCache = createLockableCache(cacheSize)

	startExpiryDaemon(timeLimit, 100)
	defer stopExpiryDaemon()

	var err error
	redisClient, err = createRedisClient(redisAddr)
	if err != nil {
		log.Fatal("Error on 'redis' connection to '", redisAddr, "' error: ", err)
	}
	defer redisClient.Close()

	if portType == "http" {
		router := createRouter()
		log.Printf("Caching HTTP redis proxy now listening on port " + portStr + "...\n")
		log.Fatal(http.ListenAndServe(":"+portStr, router))
	} else {
		// TCP listener
		err := startListener(portStr)
		if err != nil {
			log.Fatal("Error starting listener on port '", portStr, "' error: ", err)
		}
	}
}
