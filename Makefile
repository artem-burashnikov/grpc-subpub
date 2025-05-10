
up: down
	docker compose up --build -d

down:
	docker compose down -v

lint:
	make -C service lint

proto:
	make -C service protobuf

test:
	make -C service test
