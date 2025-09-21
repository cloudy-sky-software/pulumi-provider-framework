ensure::
	go mod tidy && go mod download

lint::
	golangci-lint run -c .golangci.yaml --timeout 10m

test::
	go test -v ./...
