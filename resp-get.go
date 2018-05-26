// resp-get handles the RESP protocol wrapping & unwrapping for Redis GET requests.
package main

import (
	"bytes"
	"fmt"
)

// unwrapRedisKey extracts a Redis key from a RESP-formatted byte string.
func unwrapRedisKey(key []byte) []byte {

	lines := bytes.Split(key, []byte{'\r', '\n'})

	return lines[4]
}

// wrapRedisKey wraps a simple key into a RESP-formatted GET request.
func wrapRedisKey(key string) string {

	line4 := len(key)

	// Redis GET command
	return fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", line4, key)
}

// unwrapRedisValue extracts a Redis value from a RESP-formatted byte string.
func unwrapRedisValue(val []byte) []byte {

	lines := bytes.Split(val, []byte{'\r', '\n'})

	return lines[1]
}

// wrapRedisValue wraps a simple value into a RESP-formatted return value.
func wrapRedisValue(val string) string {

	line1 := len(val)

	// Redis GET return value
	return fmt.Sprintf("$%d\r\n%s\r\n", line1, val)
}
