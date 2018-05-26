// environment handles the 12-Factor environment parameters for redis-cache.
package main

import (
	"log"
	"os"
	"strconv"
)

func getEnvironmentVariables() (redisAddr string, timeLimit int, cacheSize int, portStr string, portType string) {

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

	portType = os.Getenv("TYPE")
	if portType != "http" && portType != "tcp" {
		log.Printf("Invalid TYPE: '%s', setting to 'http'\n", portType)
		portType = "http"
	}

	return
}
