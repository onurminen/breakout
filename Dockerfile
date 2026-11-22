# Builds one service from cmd/: docker build --build-arg SERVICE=ingestion .
FROM golang:1.27 AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /service ./cmd/${SERVICE}
RUN mkdir -p /ui && if [ -d ui/dist ]; then cp -a ui/dist/. /ui/; fi

FROM gcr.io/distroless/static-debian12
COPY --from=build /service /service
COPY --from=build /ui /ui
ENTRYPOINT ["/service"]
