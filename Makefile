
up: down
	docker compose up --build -d

down:
	docker compose down -v

run-tests:
	docker run --rm apitest

test:
	make down
	make up
	@echo Waiting cluster to start && sleep 5
	make run-tests
	make down
	@echo Test finished

lint:
	make -C service lint

proto:
	make -C service protobuf

unit:
	make -C service test
