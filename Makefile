dev:
	cd deploy && docker-compose up -d milvus-standalone etcd minio redis

run:
	go run cmd/server/main.go run

ingest:
	go run cmd/server/main.go ingest --file testdata/petstore.json

test:
	go test ./...
