# ---- Build stage ----
FROM golang:1.25-alpine AS builder
WORKDIR /app

# 캐시용 모듈 다운로드
COPY go.mod go.sum ./
RUN go mod download

# 소스 복사 & 빌드
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /web-go .

# ---- Runtime stage ----
FROM alpine:3.20
WORKDIR /app
COPY --from=builder /web-go /app/web-go

# 포트 (gRPC 50051, HTTP 8080)
EXPOSE 50051 8080

# 실행
ENTRYPOINT ["/app/web-go"]