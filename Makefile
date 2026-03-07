dev:
	cd deploy && docker-compose up -d milvus-standalone etcd minio redis

run:
	go run cmd/server/main.go run

test:
	go test ./...
