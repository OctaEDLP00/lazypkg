# run with:
# docker build -t lazypkg .
# docker run -it lazypkg:latest /bin/sh

FROM golang:1.26 as build
WORKDIR /go/src/github.com/OctaEDLP00/lazypkg/
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o lazypkg

FROM alpine:3.21
RUN apk add --no-cache -U npm
WORKDIR /go/src/github.com/OctaEDLP00/lazypkg/
COPY --from=build /go/src/github.com/OctaEDLP00/lazypkg ./
COPY --from=build /go/src/github.com/OctaEDLP00/lazypkg/lazypkg /bin/
RUN echo "alias lp=lazypkg" >> ~/.profile

ENTRYPOINT [ "lazypkg" ]
