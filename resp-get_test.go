package main

import (
	"testing"
)

func TestUnwrapRedisKey(t *testing.T) {

	key := unwrapRedisKey([]byte{'*', '2', '\r', '\n', '$', '3', '\r', '\n', 'G', 'E', 'T', '\r', '\n', '$', '4', '\r', '\n', 'k', 'e', 'y', 't', '\r', '\n'})

	if string(key) != "keyt" {
		t.Errorf("Expected 'keyt'. Got '%s'", string(key))
	}
}

func TestWrapRedisKey(t *testing.T) {

	formattedKey := wrapRedisKey("keyt")

	if formattedKey != "*2\r\n$3\r\nGET\r\n$4\r\nkeyt\r\n" {
		t.Errorf("Expected '*2\r\n$3\r\nGET\r\n$4\r\nkeyt\r\n', got '%s'", formattedKey)
	}
}

func TestUnwrapRedisValue(t *testing.T) {

	value := unwrapRedisValue([]byte{'$', '6', '\r', '\n', 'v', 'a', 'l', 'u', 'e', 't', '\r', '\n'})

	if string(value) != "valuet" {
		t.Errorf("Expected 'valuet'. Got '%s'", string(value))
	}
}

func TestWrapRedisValue(t *testing.T) {

	returnVal := wrapRedisValue("valuet")

	if returnVal != "$6\r\nvaluet\r\n" {
		t.Errorf("Expected '$6\r\nvaluet\r\n'. Got '%s'", returnVal)
	}
}
