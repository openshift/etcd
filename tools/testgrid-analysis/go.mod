module go.etcd.io/etcd/tools/testgrid-analysis/v3

go 1.24.0

toolchain go1.24.11

require (
	github.com/GoogleCloudPlatform/testgrid v0.0.173
	github.com/google/go-github/v60 v60.0.0
	github.com/spf13/cobra v1.9.1
	google.golang.org/protobuf v1.36.5
)

require (
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250303144028-a0af3efb3deb // indirect
	google.golang.org/grpc v1.71.1 // indirect
)

replace golang.org/x/net => github.com/openshift-sustaining/net v0.50.0-sec.3
