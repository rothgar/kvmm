.PHONY: build run clean docker-build docker-run docker-stop

BINARY := kvmm
CONFIG := config.toml
PORT := 8080

# Build the binary
build:
	go build -o $(BINARY) .

# Run the server
run: build
	./$(BINARY) server -config $(CONFIG)

# Clean build artifacts
clean:
	rm -f $(BINARY)
	rm -rf thumbnails/

# Build Docker image
docker-build:
	docker build -t $(BINARY) .

# Run Docker container
docker-run:
	docker run -d --name $(BINARY) \
		-p $(PORT):8080 \
		-v $(PWD)/$(CONFIG):/data/config.toml \
		-v $(BINARY)-data:/data/thumbnails \
		$(BINARY)

# Stop and remove Docker container
docker-stop:
	docker stop $(BINARY) && docker rm $(BINARY)

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Download dependencies
deps:
	go mod download
	go mod tidy
