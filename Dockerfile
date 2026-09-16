FROM golang:1.27-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o ticket-system .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /app/ticket-system .

ENV PORT=8080
ENV DB_PATH=/app/data/tickets.db
RUN mkdir -p /app/data

EXPOSE 8080
CMD ["./ticket-system"]
