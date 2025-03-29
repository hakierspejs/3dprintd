FROM golang:1.23.3

WORKDIR /app

# Install dumb-init
RUN apt-get update && apt-get install -y dumb-init && rm -rf /var/lib/apt/lists/* /var/cache/apt/*

EXPOSE 5000
ENTRYPOINT ["/usr/bin/dumb-init", "--"]

# Copy Go files
COPY ./main.go .
COPY ./go.mod .

COPY secrets secrets/

RUN go get
RUN go build -o 3dprint

CMD ["./3dprint"]
