#!/bin/bash

cd frontend && npm install && npm run build && cd ..
go build -o vantaged ./cmd/vantaged
./vantaged --listen :7687