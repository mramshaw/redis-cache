MAIN		:= redis_lru_cache

all:		$(MAIN)
		@echo '$(MAIN)' has been compiled

$(MAIN):	build

.PHONY:     test, clean

test:
		docker-compose up -d redis
		docker-compose up golang
		docker-compose down

relay:
		docker-compose up -d redis
		docker-compose up golang-http
		docker-compose down

run:
		docker-compose up
		docker-compose down

build:
		docker-compose run golang go build -o $(MAIN) .
		docker-compose down

clean:
		docker-compose run golang rm -f ./$(MAIN) coverage.html coverage.txt
		docker-compose down
