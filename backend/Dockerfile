FROM golang:1.23 AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /app/server /app/server
ENV PORT=8080
EXPOSE 8080
CMD ["/app/server"]
