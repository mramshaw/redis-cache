package main

import (
	"os"
	"testing"
)

func TestEnvironmentDefaults(t *testing.T) {

	os.Clearenv()

	redisAddr, timeLimit, cacheSize, portStr, portType := getEnvironmentVariables()

	if redisAddr != "redis-backend:6379" {
		t.Errorf("Expected address 'redis-backend:6379'. Got '%s'", redisAddr)
	}

	if timeLimit != 5000 {
		t.Errorf("Expected expiry time '5000'. Got '%d'", timeLimit)
	}

	if cacheSize != 100 {
		t.Errorf("Expected cache size '100'. Got '%d'", cacheSize)
	}

	if portStr != "5000" {
		t.Errorf("Expected port '5000'. Got '%s'", portStr)
	}

	if portType != "http" {
		t.Errorf("Expected type 'http'. Got '%s'", portType)
	}
}
