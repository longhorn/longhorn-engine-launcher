package spdk

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	spdkclient "github.com/longhorn/go-spdk-helper/pkg/spdk/client"
	spdktypes "github.com/longhorn/go-spdk-helper/pkg/spdk/types"

	"github.com/longhorn/longhorn-spdk-engine/pkg/client"
	"github.com/longhorn/longhorn-spdk-engine/pkg/types"
	"github.com/longhorn/longhorn-spdk-engine/pkg/util"
)

const (
	DiskTypeFilesystem = "filesystem"
	DiskTypeBlock      = "block"

	ReplicaRebuildingLvolSuffix  = "rebuilding"
	RebuildingSnapshotNamePrefix = "rebuild"

	SyncTimeout = 60 * time.Minute
)

func GetReplicaSnapshotLvolNamePrefix(replicaName string) string {
	return fmt.Sprintf("%s-snap-", replicaName)
}

func GetReplicaSnapshotLvolName(replicaName, snapshotName string) string {
	return fmt.Sprintf("%s%s", GetReplicaSnapshotLvolNamePrefix(replicaName), snapshotName)
}

func GetSnapshotNameFromReplicaSnapshotLvolName(replicaName, snapLvolName string) string {
	return strings.TrimPrefix(snapLvolName, GetReplicaSnapshotLvolNamePrefix(replicaName))
}

func IsReplicaSnapshotLvol(replicaName, lvolName string) bool {
	return strings.HasPrefix(lvolName, GetReplicaSnapshotLvolNamePrefix(replicaName))
}

func GenerateRebuildingSnapshotName() string {
	return fmt.Sprintf("%s-%s", RebuildingSnapshotNamePrefix, util.UUID()[:8])
}

func GetReplicaRebuildingLvolName(replicaName string) string {
	return fmt.Sprintf("%s-%s", replicaName, ReplicaRebuildingLvolSuffix)
}

func GetNvmfEndpoint(nqn, ip string, port int32) string {
	return fmt.Sprintf("nvmf://%s:%d/%s", ip, port, nqn)
}

func GetServiceClient(address string) (*client.SPDKClient, error) {
	ip, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	// TODO: Can we use the fixed port
	addr := net.JoinHostPort(ip, strconv.Itoa(types.SPDKServicePort))

	// TODO: Can we share the clients in the whole server?
	return client.NewSPDKClient(addr)
}

func GetBdevMap(cli *spdkclient.Client) (map[string]*spdktypes.BdevInfo, error) {
	bdevList, err := cli.BdevGetBdevs("", 0)
	if err != nil {
		return nil, err
	}

	bdevMap := map[string]*spdktypes.BdevInfo{}
	for idx := range bdevList {
		bdev := &bdevList[idx]
		bdevType := spdktypes.GetBdevType(bdev)

		switch bdevType {
		case spdktypes.BdevTypeLvol:
			if len(bdev.Aliases) != 1 {
				continue
			}
			bdevMap[bdev.Aliases[0]] = bdev
		case spdktypes.BdevTypeRaid:
			fallthrough
		default:
			bdevMap[bdev.Name] = bdev
		}
	}

	return bdevMap, nil
}

func GetBdevLvolMap(cli *spdkclient.Client) (map[string]*spdktypes.BdevInfo, error) {
	bdevList, err := cli.BdevGetBdevs("", 0)
	if err != nil {
		return nil, err
	}

	bdevLvolMap := map[string]*spdktypes.BdevInfo{}
	for idx := range bdevList {
		bdev := &bdevList[idx]
		bdevType := spdktypes.GetBdevType(bdev)
		if bdevType != spdktypes.BdevTypeLvol {
			continue
		}
		if len(bdev.Aliases) != 1 {
			continue
		}
		lvolName := spdktypes.GetLvolNameFromAlias(bdev.Aliases[0])
		bdevLvolMap[lvolName] = bdev
	}

	return bdevLvolMap, nil
}

func GetNvmfSubsystemMap(cli *spdkclient.Client) (map[string]*spdktypes.NvmfSubsystem, error) {
	subsystemList, err := cli.NvmfGetSubsystems("", "")
	if err != nil {
		return nil, err
	}

	subsystemMap := map[string]*spdktypes.NvmfSubsystem{}
	for idx := range subsystemList {
		subsystem := &subsystemList[idx]
		subsystemMap[subsystem.Nqn] = subsystem
	}

	return subsystemMap, nil
}

type BackupCreateInfo struct {
	BackupName     string
	IsIncremental  bool
	ReplicaAddress string
}
