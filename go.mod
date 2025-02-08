module github.com/longhorn/longhorn-instance-manager

go 1.22.7

toolchain go1.23.6

require (
	github.com/google/uuid v1.6.0
	github.com/longhorn/backupstore v0.0.0-20250204055542-0f659475ac46
	github.com/longhorn/go-common-libs v0.0.0-20250204050409-8ebd4432fd70
	github.com/longhorn/go-spdk-helper v0.0.0-20240820144231-33c0873802ff
	github.com/longhorn/longhorn-engine v1.7.3-dev-20241215.0.20241216174127-932253f43c96
	github.com/longhorn/longhorn-spdk-engine v0.0.0-20240821075759-df0d2ae1b9bb
	github.com/longhorn/types v0.0.0-20241225162202-00d3a5fd7502
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.3
	github.com/urfave/cli v1.22.16
	golang.org/x/net v0.34.0
	golang.org/x/sync v0.10.0
	google.golang.org/grpc v1.69.0
	google.golang.org/protobuf v1.35.2
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c
	k8s.io/mount-utils v0.31.3
)

require (
	github.com/0xPolygon/polygon-edge v1.3.3 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.16.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.10.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.5.0 // indirect
	github.com/RoaringBitmap/roaring v1.9.4 // indirect
	github.com/aws/aws-sdk-go v1.55.6 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.12.0 // indirect
	github.com/c9s/goprocinfo v0.0.0-20210130143923-c95fcf8c64a8 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.5 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/gammazero/deque v0.2.1 // indirect
	github.com/gammazero/workerpool v1.1.3 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/gorilla/handlers v1.5.2 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/longhorn/go-iscsi-helper v0.0.0-20241216160354-6e5f17558cef // indirect
	github.com/longhorn/nsfilelock v0.0.0-20200723175406-fa7c83ad0003 // indirect
	github.com/longhorn/sparse-tools v0.0.0-20241124090628-9499c1fb63d7 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/mitchellh/go-ps v1.0.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/runc v1.1.14 // indirect
	github.com/opencontainers/runtime-spec v1.1.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/rancher/go-fibmap v0.0.0-20160418233256-5fc9f8c1ed47 // indirect
	github.com/rogpeppe/go-internal v1.12.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shirou/gopsutil/v3 v3.24.5 // indirect
	github.com/slok/goresilience v0.2.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/exp v0.0.0-20250128182459-e0ece0dbea4c // indirect
	golang.org/x/oauth2 v0.23.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/term v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	golang.org/x/time v0.7.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241015192408-796eee8c2d53 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.31.3 // indirect
	k8s.io/apimachinery v0.31.3 // indirect
	k8s.io/client-go v0.31.3 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20240228011516-70dd3763d340 // indirect
	k8s.io/utils v0.0.0-20241210054802-24370beab758 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
