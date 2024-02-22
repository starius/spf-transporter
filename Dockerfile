FROM golang:1.20 as golang-image

FROM golang-image as source-image
COPY ./ /src/spf-transporter

FROM source-image as build-image
WORKDIR /src/spf-transporter
RUN CGO_ENABLED=0 go install -mod=vendor -ldflags='-w -s -extldflags "-static"' .../cmd/transporter

FROM scratch as scratch-with-certs
COPY --from=golang-image /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

FROM scratch-with-certs as transporter
COPY --from=build-image /go/bin/transporter /go/bin/transporter
ENTRYPOINT [ "/go/bin/transporter" ]
