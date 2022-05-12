#!/bin/bash

set -x
cd /go/src || exit
go mod tidy
#Base service
go build -o /workspace/gorse-master cmd/gorse-master/main.go

go build -o /workspace/gorse-server cmd/gorse-server/main.go

go build -o /workspace/gorse-worker cmd/gorse-worker/main.go