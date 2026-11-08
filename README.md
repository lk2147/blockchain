# blockchain
## Install Golang
visit https://go.dev/doc/install
## Download Protobuf
visit https://github.com/protocolbuffers/protobuf/releases/tag/v36.2
## Environment variables
GOROOT := path to Golang folder
PROTOROOT := path to extracted Protobuf
GOPATH := path to isolated workspace/dependency cache folder
eidt PATH to include %GOROOT%\bin, %PROTOROOT%\bin, %GOPATH%\bin
## Checking (remember to restart terminal/ide before running)
go version
protoc --version
go env GOPATH
## Create go.mod
go mod init blockchain-lab01
go get google.golang.org/grpc@latest
go get google.golang.org/protobuf@latest
## Install plugins (remember to restart terminal/ide after installation)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
## Compile the Schema (.proto) into Go gRPC interfaces
protoc --go_out=paths=source_relative:. --go-grpc_out=paths=source_relative:. src/proto/consensus.proto
## How to read