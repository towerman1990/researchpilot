FROM node:22-alpine AS web
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /researchpilot ./cmd/researchpilot

FROM alpine:3.22
WORKDIR /app
COPY --from=build /researchpilot /usr/local/bin/researchpilot
COPY --from=web /web/dist /app/web/dist
COPY migrations/ /app/migrations/
USER 65532:65532
ENTRYPOINT ["researchpilot"]
