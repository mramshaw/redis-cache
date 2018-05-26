# redis-cache

[![Build status](https://travis-ci.org/mramshaw/redis-cache.svg?branch=master)](https://travis-ci.org/mramshaw/redis-cache)
[![Coverage Status](http://codecov.io/github/mramshaw/redis-cache/coverage.svg?branch=master)](http://codecov.io/github/mramshaw/redis-cache?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/mramshaw/redis-cache?style=flat-square)](https://goreportcard.com/report/github.com/mramshaw/redis-cache)
[![GoDoc](https://godoc.org/github.com/mramshaw/redis-cache?status.svg)](https://godoc.org/github.com/mramshaw/redis-cache)
[![GitHub release](https://img.shields.io/github/release/mramshaw/redis-cache.svg?style=flat-square)](https://github.com/mramshaw/redis-cache/releases)

A redis cache proxy service

There are myriad solutions for scaling out [redis](https://redis.io/).

First, a definition. According to [Wikipedia](https://en.wikipedia.org/wiki/Redis), redis is:

> an open-source in-memory database project implementing a distributed, in-memory key-value store with optional durability.

Note the _optional durability_ part: this means redis is __not__ a database. While there are
options that may be specified that will make redis _resemble_ a database, using redis as a
database will probably only lead to problems (I have heard this from more than one company).

However, I suppose I can understand the confusion; according to the same Wikipedia article:

> Redis has also been ranked the #4 NoSQL database in user satisfaction and market presence based on user reviews

The _distributed_ part bears some discussion as well. There are ways of replicating and/or
sharding redis that allow for the creation of redis clusters (of up to 1,000 nodes), but this
effort attempts to create redis caches that will offload GET requests from the redis master.

The ultimate goal is that these caches should be _composable_, i.e. it should be possible
to layer them one on top of another so as to create a federated cache. According, they will
either serve GET requests from local cache memory or pass them on to the next redis cache.
Ultimately these GET requests are served by the redis master itself.


## Deployment

The application will be deployed as follows:

![Deployment](images/deployment.png)

## What the code does

The application launches a web server which responds to HTTP requests.

When a request for a specific key is received, the application checks
its cache for the specified key. If found, the value associated with
the key is returned via HTTP. If not found, the request is forwarded
to Redis - if the key is found then the value is stored in the cache
and the value is returned via HTTP. If not found a 404 is returned.

At application startup a daemon process is launched, which runs from
time to time and expires any cache entries older than a configurable
time limit.

The size of the cache may also be specified; only this number of
entries may be stored in the cache, with older entries being evicted
to make space for newer entries.

#### RESP

Internally, redis uses its own wire protocol. See the following link
for details:

    https://redis.io/topics/protocol

## How to run

1. Download the repo.

2. Unzip it somewhere.

3. Change directory into the repo:

    $ cd assignment

4. Type the following to run the tests:

    $ make test

[It may take a few minutes for the docker images to download.]

The results should look as follows (times are GMT):

```
$ make test
docker-compose up -d redis
Creating network "rediscache_redis-caching" with the default driver
Creating rediscache_redis_1 ... 
Creating rediscache_redis_1 ... done
docker-compose up golang
rediscache_redis_1 is up-to-date
Creating rediscache_golang_1 ... 
Creating rediscache_golang_1 ... done
Attaching to rediscache_golang_1
golang_1  | Reformatting source code ...
golang_1  | Vetting source code ...
golang_1  | Testing source code ...
golang_1  | 2018/04/14 20:57:57 Running setUpTestData
golang_1  | === RUN   TestHealthCheck
golang_1  | --- PASS: TestHealthCheck (0.00s)
golang_1  | === RUN   TestCacheHit
golang_1  | --- PASS: TestCacheHit (0.00s)
golang_1  | === RUN   TestCacheMiss
golang_1  | --- PASS: TestCacheMiss (0.00s)
golang_1  | === RUN   TestGetExistingRedisKey
golang_1  | --- PASS: TestGetExistingRedisKey (0.00s)
golang_1  | === RUN   TestGetNonexistentRedisKey
golang_1  | --- PASS: TestGetNonexistentRedisKey (0.00s)
golang_1  | === RUN   TestGetExpiredCacheKey
golang_1  | --- PASS: TestGetExpiredCacheKey (5.40s)
golang_1  | === RUN   TestGetExpiredRedisKey
golang_1  | --- PASS: TestGetExpiredRedisKey (6.00s)
golang_1  | === RUN   TestGetTouchedCacheKey
golang_1  | --- PASS: TestGetTouchedCacheKey (11.41s)
golang_1  | PASS
golang_1  | 2018/04/14 20:58:19 Running tearDownTestData
golang_1  | ok  	redis-cache	22.828s
rediscache_golang_1 exited with code 0
docker-compose down
Stopping rediscache_redis_1 ... done
Removing rediscache_golang_1 ... done
Removing rediscache_redis_1 ... done
Removing network rediscache_redis-caching
$
```

## Dependencies

The various libraries are bundled in a __vendor__ directory.

This is perhaps not ideal but they would need to be downloaded
in any case, and it is always nice to have all dependencies
bundled with source code.

The dependencies are sourced as follows:

``` Bash
$ GOPATH=`pwd`/vendor/ go get -d -v .
```

## Testing

For testing, `redis-cli` is needed. Set up test data as follows:

```
$ redis-cli
127.0.0.1:6379> SET keyt valuet
OK
127.0.0.1:6379> exit
$
```

The HTTP version (default) requires `curl` for testing.

The TCP version requires `nc` (netcat) for testing.

1. docker-compose up -d redis
2. docker-compose up golang
3. docker-compose down

#### Build

Build `redis_lru_cache` (this will save time later).

```
$ make
<...>
redis_lru_cache has been compiled
$
```

#### Redis

Verify redis is running correctly as follows:

```
$ docker-compose up -d redis
```

And:

```
$ cat test | nc localhost 6379
$6
valuet
$
```

#### TCP

Test as follows:

```
$ docker-compose up golang-tcp
<...>
```

In a new console:

```
$ cat test | nc localhost 7001
valuet$
```

#### HTTP

Test as follows:


```
$ docker-compose up golang-http
<...>
```

In a new console:

```
$ curl http://localhost/keyt
valuet$
```

## To Do

- [ ] Refactor to avoid duplicate mutexes
- [ ] Refactor duplicated tests and testing code (table-driven)
- [x] Refactor to include 12-Factor initialization in code coverage
- [ ] Add goroutines for multiple clients ([pool](https://godoc.org/github.com/mediocregopher/radix.v2/pool) looks useful)
- [x] Add RESP ([respgo](https://github.com/teambition/respgo) looks useful)
- [ ] Add pipelining
