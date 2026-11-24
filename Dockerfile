# 画面（web/）を型チェックしてビルドする。出力は internal/webapp/dist（Go のバイナリに埋め込む）
FROM oven/bun:1.4 AS web
WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
ARG VITE_SOURCE_REPOSITORY
ARG VITE_SOURCE_REF
RUN bun run typecheck && bun run build

FROM golang:1.25.0-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
# go.mod の replace が指す同梱ライブラリも先に入れる
COPY third_party/ ./third_party/
RUN go mod download
COPY . .
COPY --from=web /app/internal/webapp/dist ./internal/webapp/dist
RUN CGO_ENABLED=0 go build -o ryudai-tools ./cmd/ryudai-tools/

FROM alpine:3.20
RUN addgroup -S ryudai && adduser -S ryudai -G ryudai \
 && mkdir -p /data && chown ryudai:ryudai /data
COPY --from=builder /app/ryudai-tools /usr/local/bin/ryudai-tools
COPY LICENSE /usr/share/licenses/ryudai-tools/LICENSE
COPY third_party/ledongthuc-pdf/LICENSE /usr/share/licenses/ryudai-tools/LICENSE.ledongthuc-pdf
USER ryudai
WORKDIR /data
EXPOSE 8080
CMD ["/usr/local/bin/ryudai-tools"]
