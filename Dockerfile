FROM golang:1.16.5-alpine3.14 as builder
WORKDIR $GOPATH/src/github.com/sapcc/pagerduty2slack
RUN apk add --no-cache make git
COPY . .
ARG VERSION
RUN go mod download
RUN go build -o ./bin/pagerduty2slack ./cmd/pagerduty2slack.go
RUN make build
#RUN ls -lisa ./bin

FROM alpine:3.14
LABEL maintainer="Tilo Geissler <tilo.geissler@@sap.com>"
LABEL source_repository="https://github.com/sapcc/pagerduty2slack"

RUN apk add --no-cache curl
COPY --from=builder /go/src/github.com/sapcc/pagerduty2slack/bin/pagerduty2slack /usr/local/bin/
CMD ["pagerduty2slack","-config","/etc/config/_run_config.yaml"]
