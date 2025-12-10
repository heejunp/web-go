# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# go.mod와 go.sum 복사
COPY go.mod ./

COPY . .

# 의존성 다운로드
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o myapp main.go

# 2. Run Stage
FROM alpine:latest
WORKDIR /root/

COPY --from=builder /app/myapp .

# 파일 생성 테스트용 디렉토리
RUN mkdir -p /files/pv /files/pod

EXPOSE 8080
CMD ["./myapp"]