FROM golang:1.11.0-alpine3.8 as builder
WORKDIR /go/src/github.com/sapcc/pagerduty2slack
RUN apk add --no-cache make
COPY . .
ARG VERSION
RUN make all

FROM alpine:3.8
LABEL maintainer="Tilo Geissler <tilo.geissler@@sap.com>"

RUN apk add --no-cache curl
RUN curl -Lo /bin/dumb-init https://github.com/Yelp/dumb-init/releases/download/v1.2.0/dumb-init_1.2.0_amd64 \
	&& chmod +x /bin/dumb-init \
	&& dumb-init -V
COPY --from=builder /go/src/github.com/sapcc/pagerduty2slack/bin/linux/* /usr/local/bin/
ENTRYPOINT ["dumb-init", "--"]
CMD ["pagerduty2slack"]
