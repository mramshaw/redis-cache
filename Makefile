MAIN		:= redis_lru_cache

all:		$(MAIN)
		@echo '$(MAIN)' has been compiled

$(MAIN):	build

test:
		docker-compose up -d redis
		docker-compose up golang
		docker-compose down

run:
		docker-compose up
		docker-compose down

build:
		docker-compose run golang go build -o $(MAIN) .
		docker-compose down

clean:
		docker-compose run golang rm ./$(MAIN)
		docker-compose down
