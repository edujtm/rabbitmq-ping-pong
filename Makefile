
RABBIT_URL = 'amqp://localhost:5672'
PINGER_PORT = '3300'
PONGER_PORT = '3400'

.PHONY: build-pinger-image build-ponger-image build-images run


run:
	docker-compose up

run-pinger:
	go run ./cmd/pinger/main.go --rabbit-url $(RABBIT_URL) --port $(PINGER_PORT)

run-ponger:
	go run ./cmd/ponger/main.go --rabbit-url $(RABBIT_URL) --port $(PONGER_PORT)

build-and-run: build-images run

build-images: build-pinger-image build-ponger-image

build-pinger-image:
	docker build -f Dockerfile.ping -t pinger .


build-ponger-image:
	docker build -f Dockerfile.pong -t ponger .