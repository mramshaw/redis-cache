#!/bin/sh
# run-cache-tests.sh

echo "Reformatting source code ..."
# .go files are reformatted to conform to gofmt standards
GOOS=linux gofmt -d -e -s -w *.go

echo "Vetting source code ..."
GOOS=linux go tool vet *.go

echo "Testing source code ..."
GOOS=linux go test -coverprofile=coverage.txt -covermode=atomic -v .
GOOS=linux go tool cover -html=coverage.txt -o coverage.html
