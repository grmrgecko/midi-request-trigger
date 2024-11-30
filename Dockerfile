FROM golang:1.20

# Build app
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN go build -o /midi-request-trigger
WORKDIR /app
RUN rm -Rf /app; mkdir /etc/midi-request-trigger

# Configuration volume
VOLUME ["/etc/midi-request-trigger"]

# Command
CMD ["/midi-request-trigger"]
