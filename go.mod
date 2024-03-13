module github.com/longhorn/longhorn-instance-manager

go 1.21

require (
	github.com/RoaringBitmap/roaring v1.9.0
	github.com/google/uuid v1.6.0
	github.com/longhorn/backupstore v0.0.0-20240219094812-3a87ee02df77
	github.com/longhorn/go-common-libs v0.0.0-20240307063052-6e77996eda29
	github.com/longhorn/go-spdk-helper v0.0.0-20240308030201-9b252d6f7250
	github.com/longhorn/longhorn-engine v1.6.0-dev-20240105.0.20240126141003-067f67803ee8
	github.com/longhorn/longhorn-spdk-engine v0.0.0-20240308142500-50ded5048249
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.3
	github.com/urfave/cli v1.22.14
	golang.org/x/net v0.22.0
	golang.org/x/sync v0.6.0
	google.golang.org/grpc v1.62.1
	google.golang.org/protobuf v1.33.0
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c
	k8s.io/mount-utils v0.29.2
)

require (
	github.com/0xPolygon/polygon-edge v1.3.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v0.21.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v0.8.3 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v0.3.0 // indirect
	github.com/aws/aws-sdk-go v1.46.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.12.0 // indirect
	github.com/c9s/goprocinfo v0.0.0-20210130143923-c95fcf8c64a8 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/felixge/httpsnoop v1.0.1 // indirect
	github.com/gammazero/deque v0.2.1 // indirect
	github.com/gammazero/workerpool v1.1.3 // indirect
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/gofrs/flock v0.8.1 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/gorilla/handlers v1.5.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/longhorn/nsfilelock v0.0.0-20200723175406-fa7c83ad0003 // indirect
	github.com/longhorn/sparse-tools v0.0.0-20230408015858-c849def39d3c // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/go-ps v1.0.0 // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.17 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/client_golang v1.17.0 // indirect
	github.com/prometheus/client_model v0.4.1-0.20230718164431-9a2bf3000d16 // indirect
	github.com/prometheus/common v0.44.0 // indirect
	github.com/prometheus/procfs v0.11.1 // indirect
	github.com/rancher/go-fibmap v0.0.0-20160418233256-5fc9f8c1ed47 // indirect
	github.com/rogpeppe/go-internal v1.10.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shirou/gopsutil/v3 v3.24.2 // indirect
	github.com/slok/goresilience v0.2.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240123012728-ef4313101c80 // indirect
	k8s.io/apimachinery v0.29.2 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b // indirect
)

replace github.com/longhorn/longhorn-spdk-engine => github.com/nirvati/longhorn-spdk-engine v0.0.0-20240224230411-48b5ce1f7748
