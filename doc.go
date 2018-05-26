/*
redis-cache is a composable redis caching proxy.

Specifically, it caches Redis GET requests, ideally off-loading processing from the Redis master.

These caching proxies can be stacked to add capacity to a Redis master while reducing load.

Environmental parameters:

    REDIS specifies the downstream Redis master (which might be another caching proxy)

    EXPIRY_TIME specifies the length of time (milliseconds) the Redis value should be cached

    CACHE_SIZE defines the number of Redis values to cache

    PORT specifies the port on which the caching instance should listen

    TYPE specifies the type of caching to provide (either HTTP or TCP)
*/
package main
