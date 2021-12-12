FROM golang:1.17.3-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /go/src/github.com/trapped/sx

COPY . .

RUN CGO_ENABLED=0 go build -o build/sx ./cmd/sx

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /go/src/github.com/trapped/sx/build/sx /bin/sx

CMD ["sx"]
