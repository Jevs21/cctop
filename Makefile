BINARY := cctop-go
CMD    := ./cmd/cctop

.PHONY: build test run clean

build:
	go build -o $(BINARY) $(CMD)

test:
	go test -v ./...

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
