package main

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

var router *mux.Router

func TestMain(m *testing.M) {

	// We will not use any of these, including for code coverage purposes.
	redisAddr, timeLimit, cacheSize, portStr := getEnvironmentVariables()
	log.Printf("Caching redis: %s, expiry=%d, cache size=%d, port=%s\n", redisAddr, timeLimit, cacheSize, portStr)

	client = createRedisClient("redis-backend:6379")
	defer client.Close()

	// Set up some data in Redis backend
	setUpTestData()

	redisCache = createLockableCache(50)
	router = createRouter()
	code := m.Run()

	// Clear data from Redis backend
	tearDownTestData()
	os.Exit(code)
}

func TestHealthCheck(t *testing.T) {

	req, err := http.NewRequest("GET", "/ping", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != "PONG" {
		t.Errorf("Expected 'PONG'. Got '%s'", body)
	}
}

func TestCacheHit(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Cache size is 50; load 100 entries
	loadCache(t)

	cacheSize := redisCache.lru.Len()
	if cacheSize != 50 {
		t.Errorf("Expected cache size '50'. Got '%d'", cacheSize)
	}

	req, err := http.NewRequest("GET", "/key51", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != "value51" {
		t.Errorf("Expected 'value51'. Got '%s'", body)
	}

	if cacheHit != 1 {
		t.Errorf("Expected cacheHit '1'. Got '%d'", cacheHit)
	}
	if cacheMiss != 100 {
		t.Errorf("Expected cacheMiss '100'. Got '%d'", cacheMiss)
	}

	redisCache.lru.Purge()
	clearCacheStats()
}

func loadCache(t *testing.T) {

	for i := 1; i <= 100; i++ {
		iStr := strconv.Itoa(i)
		key := "/key" + iStr
		value := "value" + iStr
		req, err := http.NewRequest("GET", key, nil)
		if err != nil {
			t.Errorf("Error on http.NewRequest: %s", err)
		}
		response := executeRequest(req)
		checkResponseCode(t, http.StatusOK, response.Code)
		if body := response.Body.String(); body != value {
			t.Errorf("Expected '%s'. Got '%s'", value, body)
		}
	}
}

func TestCacheMiss(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Cache size is 50; load 100 entries
	loadCache(t)

	cacheSize := redisCache.lru.Len()
	if cacheSize != 50 {
		t.Errorf("Expected cache size '50'. Got '%d'", cacheSize)
	}

	req, err := http.NewRequest("GET", "/key50", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != "value50" {
		t.Errorf("Expected 'value50'. Got '%s'", body)
	}

	if cacheHit != 0 {
		t.Errorf("Expected cacheHit '0'. Got '%d'", cacheHit)
	}
	if cacheMiss != 101 {
		t.Errorf("Expected cacheMiss '101'. Got '%d'", cacheMiss)
	}

	redisCache.lru.Purge()
	clearCacheStats()
}

func TestGetExistingRedisKey(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	req, err := http.NewRequest("GET", "/key1", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != "value1" {
		t.Errorf("Expected 'value1'. Got '%s'", body)
	}

	if cacheHit != 0 {
		t.Errorf("Expected cacheHit '0'. Got '%d'", cacheHit)
	}
	if cacheMiss != 1 {
		t.Errorf("Expected cacheMiss '1'. Got '%d'", cacheMiss)
	}

	redisCache.lru.Purge()
	clearCacheStats()
}

func TestGetNonexistentRedisKey(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	req, err := http.NewRequest("GET", "/doesNotExist", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusNotFound, response.Code)

	if body := response.Body.String(); body != "" {
		t.Errorf("Expected ''. Got '%s'", body)
	}

	if cacheHit != 0 {
		t.Errorf("Expected cacheHit '0'. Got '%d'", cacheHit)
	}
	// Qualifies as a cache miss even though cache will not be updated
	if cacheMiss != 1 {
		t.Errorf("Expected cacheMiss '1'. Got '%d'", cacheMiss)
	}

	cacheSize := redisCache.lru.Len()
	if cacheSize != 0 {
		t.Errorf("Expected cache size '0'. Got '%d'", cacheSize)
	}
	redisCache.lru.Purge()
	clearCacheStats()
}

func TestGetExpiredCacheKey(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()
	startExpiryDaemon(5000, 200)
	defer stopExpiryDaemon()

	key := "expiringCacheKey"
	val := "no expiry"
	err := client.Cmd("SET", key, val).Err
	if err != nil {
		log.Println("Error on TestGetExpiredCacheKey SET '", key, "' to '", val, "': ", err)
	}

	req, err := http.NewRequest("GET", "/expiringCacheKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	// Now sleep for 5+ seconds, after which key should be expired
	//
	// It needs to be 5+ seconds, rather than 5 seconds, to allow
	//  cache enough time (including cache expiry interval) to
	//  expire the key. An extra 210 milliseconds seems to do it.
	time.Sleep(5210 * time.Millisecond)

	req, err = http.NewRequest("GET", "/expiringCacheKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response = executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	if cacheHit != 0 {
		t.Errorf("Expected cacheHit '0'. Got '%d'", cacheHit)
	}
	if cacheMiss != 2 {
		t.Errorf("Expected cacheMiss '2'. Got '%d'", cacheMiss)
	}
	redisCache.lru.Purge()
	clearCacheStats()
}

func TestGetExpiredRedisKey(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Start the proxy expiry daemon so that
	//  we do not get stale cache entries.
	startExpiryDaemon(5000, 200)
	defer stopExpiryDaemon()

	key := "expiringRedisKey"
	val := "6 seconds"
	err := client.Cmd("SET", key, val, "EX", 6).Err
	if err != nil {
		log.Println("Error on TestGetExpiredRedisKey SET '", key, "' to '", val, "': ", err)
	}

	req, err := http.NewRequest("GET", "/expiringRedisKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	// Now sleep for 6+ seconds, after which key should be expired
	//
	// It needs to be longer than the cache expiry time (5 seconds)
	//  otherwise we will simply get a cached value.
	//
	// It needs to be 6+ seconds, rather than 6 seconds, to allow
	//  Redis enough time to expire the key. An extra millisecond
	//  seems to be enough, but allow 5 to be on the safe side.
	time.Sleep(6005 * time.Millisecond)

	req, err = http.NewRequest("GET", "/expiringRedisKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response = executeRequest(req)
	checkResponseCode(t, http.StatusNotFound, response.Code)

	if body := response.Body.String(); body != "" {
		t.Errorf("Expected ''. Got '%s'", body)
	}

	if cacheHit != 0 {
		t.Errorf("Expected cacheHit '0'. Got '%d'", cacheHit)
	}
	if cacheMiss != 2 {
		t.Errorf("Expected cacheMiss '2'. Got '%d'", cacheMiss)
	}
	redisCache.lru.Purge()
	clearCacheStats()
}

func TestGetTouchedCacheKey(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Start the proxy expiry daemon so that
	//  we do not get stale cache entries.
	startExpiryDaemon(5000, 200)
	defer stopExpiryDaemon()

	key := "touchedCacheKey"
	val := "no expiry"
	err := client.Cmd("SET", key, val).Err
	if err != nil {
		log.Println("Error on TestGetTouchedCacheKey SET '", key, "' to '", val, "': ", err)
	}

	req, err := http.NewRequest("GET", "/touchedCacheKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response := executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	// Sleep for 3 seconds
	time.Sleep(3000 * time.Millisecond)

	// There should be 2 seconds left on the cache entry expiry timer;
	//  this should reset the entry's expiry timer to 5 seconds.
	req, err = http.NewRequest("GET", "/touchedCacheKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response = executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	// Now sleep for 3 seconds, after which key should NOT be expired
	time.Sleep(3000 * time.Millisecond)

	req, err = http.NewRequest("GET", "/touchedCacheKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response = executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	// Now sleep for 5+ seconds (including cache expiry
	//  interval), after which key should be expired
	//  in the cache (but not in Redis)
	time.Sleep(5210 * time.Millisecond)

	req, err = http.NewRequest("GET", "/touchedCacheKey", nil)
	if err != nil {
		t.Errorf("Error on http.NewRequest: %s", err)
	}
	response = executeRequest(req)
	checkResponseCode(t, http.StatusOK, response.Code)

	if body := response.Body.String(); body != val {
		t.Errorf("Expected '%s'. Got '%s'", val, body)
	}

	if cacheHit != 2 {
		t.Errorf("Expected cacheHit '2'. Got '%d'", cacheHit)
	}
	if cacheMiss != 2 {
		t.Errorf("Expected cacheMiss '2'. Got '%d'", cacheMiss)
	}
	redisCache.lru.Purge()
	clearCacheStats()
}

func setUpTestData() {

	log.Printf("Running setUpTestData")

	err := client.Cmd("FLUSHDB").Err
	if err != nil {
		log.Println("Error on setUpTestData FLUSHDB: ", err)
	}

	for i := 1; i <= 100; i++ {
		iStr := strconv.Itoa(i)
		key := "key" + iStr
		value := "value" + iStr
		err = client.Cmd("SET", key, value).Err
		if err != nil {
			log.Println("Error on setUpTestData SET ", key, " to ", value, ": ", err)
		}
	}
}

func tearDownTestData() {

	log.Printf("Running tearDownTestData")

	err := client.Cmd("FLUSHDB").Err
	if err != nil {
		log.Println("Error on setUpTestData FLUSHDB: ", err)
	}
}

func executeRequest(req *http.Request) *httptest.ResponseRecorder {

	//log.Printf("Running executeRequest")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func checkResponseCode(t *testing.T, expected, actual int) {

	//log.Printf("Running checkResponseCode")

	if expected != actual {
		t.Errorf("Expected response code %d. Got %d\n", expected, actual)
	}
}
