package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
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

	// We will only use 'redisAddr', the rest are included for code coverage purposes.
	redisAddr, timeLimit, cacheSize, portStr, portType := getEnvironmentVariables()
	log.Printf("Caching redis: %s, expiry=%d, cache size=%d, port=%s, type=%s\n", redisAddr, timeLimit, cacheSize, portStr, portType)
	go startListener("5000")

	redisClient, _ = createRedisClient(redisAddr)
	defer redisClient.Close()

	// Set up some data in Redis backend
	setUpTestData()

	redisCache = createLockableCache(50)
	router = createRouter()
	code := m.Run()

	// Clear data from Redis backend
	tearDownTestData()
	os.Exit(code)
}

func TestBadListenerConfig(t *testing.T) {

	err := startListener("99999") // Bad port!
	log.Println(err)
	if err == nil {
		t.Fatal(err)
	}
}

func TestBadRedisConfig(t *testing.T) {

	_, err := createRedisClient("does-not-exist:9999")
	log.Println(err)
	if err == nil {
		t.Fatal(err)
	}
}

func TestCacheMissTCP(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Cache size is 50; load 100 entries
	loadCache(t)

	cacheSize := redisCache.lru.Len()
	if cacheSize != 50 {
		t.Errorf("Expected cache size '50'. Got '%d'", cacheSize)
	}

	clientConn, serverConn := net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err := fmt.Fprintf(clientConn, getRedisString("key50"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := string(buf[:])
	if message != "value50" {
		t.Errorf("Expected 'value50'. Got '%s'", message)
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

func TestGetExistingRedisKeyTCP(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	clientConn, serverConn := net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err := fmt.Fprintf(clientConn, getRedisString("key1"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := string(buf[:])
	if message != "value1" {
		t.Errorf("Expected 'value1'. Got '%s'", message)
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

func TestGetNonexistentRedisKeyTCP(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	clientConn, serverConn := net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err := fmt.Fprintf(clientConn, getRedisString("doesNotExist"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := string(buf[:])
	if message != "" {
		t.Errorf("Expected ''. Got '%s'", message)
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

func TestGetExpiredCacheKeyTCP(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()
	startExpiryDaemon(5000, 200)
	defer stopExpiryDaemon()

	key := "expiringCacheKey"
	val := "no expiry"
	err := redisClient.Cmd("SET", key, val).Err
	if err != nil {
		log.Println("Error on TestGetExpiredCacheKey SET '", key, "' to '", val, "': ", err)
	}

	clientConn, serverConn := net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("expiringCacheKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
	}

	// Now sleep for 5+ seconds, after which key should be expired
	//
	// It needs to be 5+ seconds, rather than 5 seconds, to allow
	//  cache enough time (including cache expiry interval) to
	//  expire the key. An extra 210 milliseconds seems to do it.
	time.Sleep(5210 * time.Millisecond)

	clientConn, serverConn = net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("expiringCacheKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err = ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message = string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
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

func TestGetExpiredRedisKeyTCP(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Start the proxy expiry daemon so that
	//  we do not get stale cache entries.
	startExpiryDaemon(5000, 200)
	defer stopExpiryDaemon()

	key := "expiringRedisKey"
	val := "6 seconds"
	err := redisClient.Cmd("SET", key, val, "EX", 6).Err
	if err != nil {
		log.Println("Error on TestGetExpiredRedisKey SET '", key, "' to '", val, "': ", err)
	}

	clientConn, serverConn := net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("expiringRedisKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
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

	clientConn, serverConn = net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("expiringRedisKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err = ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message = string(buf[:])
	if message != "" {
		t.Errorf("Expected ''. Got '%s'", message)
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

func TestGetTouchedCacheKeyTCP(t *testing.T) {

	clearCacheStats()
	redisCache.lru.Purge()

	// Start the proxy expiry daemon so that
	//  we do not get stale cache entries.
	startExpiryDaemon(5000, 200)
	defer stopExpiryDaemon()

	key := "touchedCacheKey"
	val := "no expiry"
	err := redisClient.Cmd("SET", key, val).Err
	if err != nil {
		log.Println("Error on TestGetTouchedCacheKeyTCP SET '", key, "' to '", val, "': ", err)
	}

	clientConn, serverConn := net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("touchedCacheKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err := ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
	}

	// Sleep for 3 seconds
	time.Sleep(3000 * time.Millisecond)

	// There should be 2 seconds left on the cache entry expiry timer;
	//  this should reset the entry's expiry timer to 5 seconds.
	clientConn, serverConn = net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("touchedCacheKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err = ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message = string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
	}

	// Now sleep for 3 seconds, after which key should NOT be expired
	time.Sleep(3000 * time.Millisecond)

	clientConn, serverConn = net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("touchedCacheKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err = ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message = string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
	}

	// Now sleep for 5+ seconds (including cache expiry
	//  interval), after which key should be expired
	//  in the cache (but not in Redis)
	time.Sleep(5210 * time.Millisecond)

	clientConn, serverConn = net.Pipe()
	// Pipe is in-memory but good practice to close
	defer clientConn.Close()
	defer serverConn.Close()

	go handleRequest(serverConn)
	_, err = fmt.Fprintf(clientConn, getRedisString("touchedCacheKey"))
	if err != nil {
		t.Fatal(err)
	}

	buf, err = ioutil.ReadAll(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message = string(buf[:])
	if message != val {
		t.Errorf("Expected '%s'. Got '%s'", val, message)
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

func getRedisString(key string) string {

	line4 := len(key)

	// Redis GET command
	return fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", line4, key)
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
	err := redisClient.Cmd("SET", key, val).Err
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
	err := redisClient.Cmd("SET", key, val, "EX", 6).Err
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
	err := redisClient.Cmd("SET", key, val).Err
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

	err := redisClient.Cmd("FLUSHDB").Err
	if err != nil {
		log.Println("Error on setUpTestData FLUSHDB: ", err)
	}

	for i := 1; i <= 100; i++ {
		iStr := strconv.Itoa(i)
		key := "key" + iStr
		value := "value" + iStr
		err = redisClient.Cmd("SET", key, value).Err
		if err != nil {
			log.Println("Error on setUpTestData SET ", key, " TO ", value, ": ", err)
		}
	}
}

func tearDownTestData() {

	log.Printf("Running tearDownTestData")

	err := redisClient.Cmd("FLUSHDB").Err
	if err != nil {
		log.Println("Error on tearDownTestData FLUSHDB: ", err)
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
