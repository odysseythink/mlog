#!/bin/sh

protoc --go_out=. --plugin=protoc-gen-go=$GOPATH/bin/protoc-gen-go bot.proto

protoc -I ./ --cpp_out=/home/rw/workspace/go_work/gopath/src/demo.com/nlp_demo/KnowledgeBaseParseGui bot.proto