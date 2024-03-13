package client

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/longhorn/longhorn-spdk-engine/pkg/api"
	"github.com/longhorn/longhorn-spdk-engine/pkg/util"
	"github.com/longhorn/longhorn-spdk-engine/proto/spdkrpc"
)

func (c *SPDKServiceContext) Close() error {
	if c.cc != nil {
		if err := c.cc.Close(); err != nil {
			return err
		}
		c.cc = nil
	}
	return nil
}

func (c *SPDKClient) getSPDKServiceClient() spdkrpc.SPDKServiceClient {
	return c.service
}

func NewSPDKClient(serviceURL string) (*SPDKClient, error) {
	getSPDKServiceContext := func(serviceUrl string) (SPDKServiceContext, error) {
		connection, err := grpc.Dial(serviceUrl, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return SPDKServiceContext{}, errors.Wrapf(err, "cannot connect to SPDKService %v", serviceUrl)
		}

		return SPDKServiceContext{
			cc:      connection,
			service: spdkrpc.NewSPDKServiceClient(connection),
		}, nil
	}

	serviceContext, err := getSPDKServiceContext(serviceURL)
	if err != nil {
		return nil, err
	}

	return &SPDKClient{
		serviceURL:         serviceURL,
		SPDKServiceContext: serviceContext,
	}, nil
}

func (c *SPDKClient) ReplicaCreate(name, lvsName, lvsUUID string, specSize uint64, exposeRequired bool, portCount int32) (*api.Replica, error) {
	if name == "" || lvsName == "" || lvsUUID == "" {
		return nil, fmt.Errorf("failed to start SPDK replica: missing required parameters")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.ReplicaCreate(ctx, &spdkrpc.ReplicaCreateRequest{
		Name:           name,
		LvsName:        lvsName,
		LvsUuid:        lvsUUID,
		SpecSize:       specSize,
		ExposeRequired: exposeRequired,
		PortCount:      portCount,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to start SPDK replica")
	}

	return api.ProtoReplicaToReplica(resp), nil
}

func (c *SPDKClient) ReplicaDelete(name string, cleanupRequired bool) error {
	if name == "" {
		return fmt.Errorf("failed to delete SPDK replica: missing required parameter")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaDelete(ctx, &spdkrpc.ReplicaDeleteRequest{
		Name:            name,
		CleanupRequired: cleanupRequired,
	})
	return errors.Wrapf(err, "failed to delete SPDK replica %v", name)
}

func (c *SPDKClient) ReplicaGet(name string) (*api.Replica, error) {
	if name == "" {
		return nil, fmt.Errorf("failed to get SPDK replica: missing required parameter")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.ReplicaGet(ctx, &spdkrpc.ReplicaGetRequest{
		Name: name,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get SPDK replica %v", name)
	}
	return api.ProtoReplicaToReplica(resp), nil
}

func (c *SPDKClient) ReplicaList() (map[string]*api.Replica, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.ReplicaList(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list SPDK replicas")
	}

	res := map[string]*api.Replica{}
	for replicaName, r := range resp.Replicas {
		res[replicaName] = api.ProtoReplicaToReplica(r)
	}
	return res, nil
}

func (c *SPDKClient) ReplicaWatch(ctx context.Context) (*api.ReplicaStream, error) {
	client := c.getSPDKServiceClient()
	stream, err := client.ReplicaWatch(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open replica watch stream")
	}

	return api.NewReplicaStream(stream), nil
}

func (c *SPDKClient) ReplicaSnapshotCreate(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to create SPDK replica snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaSnapshotCreate(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to create SPDK replica %s snapshot %s", name, snapshotName)
}

func (c *SPDKClient) ReplicaSnapshotDelete(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to delete SPDK replica snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaSnapshotDelete(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to delete SPDK replica %s snapshot %s", name, snapshotName)
}

func (c *SPDKClient) ReplicaSnapshotRevert(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to revert SPDK replica snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaSnapshotRevert(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to revert SPDK replica %s snapshot %s", name, snapshotName)
}

func (c *SPDKClient) ReplicaRebuildingSrcStart(srcReplicaName, dstReplicaName, dstRebuildingLvolAddress string) error {
	if srcReplicaName == "" {
		return fmt.Errorf("failed to start replica rebuilding src: missing required parameter src replica name")
	}
	if dstReplicaName == "" || dstRebuildingLvolAddress == "" {
		return fmt.Errorf("failed to start replica rebuilding src: missing required parameter dst replica name or dst rebuilding lvol address")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingSrcStart(ctx, &spdkrpc.ReplicaRebuildingSrcStartRequest{
		Name:                     srcReplicaName,
		DstReplicaName:           dstReplicaName,
		DstRebuildingLvolAddress: dstRebuildingLvolAddress,
	})
	return errors.Wrapf(err, "failed to start replica rebuilding src %s for rebuilding replica %s", srcReplicaName, dstReplicaName)
}

func (c *SPDKClient) ReplicaRebuildingSrcFinish(srcReplicaName, dstReplicaName string) error {
	if srcReplicaName == "" {
		return fmt.Errorf("failed to finish replica rebuilding src: missing required parameter src replica name")
	}
	if dstReplicaName == "" {
		return fmt.Errorf("failed to finish replica rebuilding src: missing required parameter dst replica name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingSrcFinish(ctx, &spdkrpc.ReplicaRebuildingSrcFinishRequest{
		Name:           srcReplicaName,
		DstReplicaName: dstReplicaName,
	})
	return errors.Wrapf(err, "failed to finish replica rebuilding src %s for rebuilding replica %s", srcReplicaName, dstReplicaName)
}

func (c *SPDKClient) ReplicaRebuildingSrcAttach(srcReplicaName, dstReplicaName, dstRebuildingLvolAddress string) error {
	if srcReplicaName == "" {
		return fmt.Errorf("failed to attach replica rebuilding src: missing required parameter src replica name")
	}
	if dstReplicaName == "" || dstRebuildingLvolAddress == "" {
		return fmt.Errorf("failed to attach replica rebuilding src: missing required parameter dst replica name or dst rebuilding lvol address")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingSrcAttach(ctx, &spdkrpc.ReplicaRebuildingSrcAttachRequest{
		Name:                     srcReplicaName,
		DstReplicaName:           dstReplicaName,
		DstRebuildingLvolAddress: dstRebuildingLvolAddress,
	})
	return errors.Wrapf(err, "failed to attach replica rebuilding src %s for rebuilding replica %s", srcReplicaName, dstReplicaName)
}

func (c *SPDKClient) ReplicaRebuildingSrcDetach(srcReplicaName, dstReplicaName string) error {
	if srcReplicaName == "" {
		return fmt.Errorf("failed to detach replica rebuilding src: missing required parameter src replica name")
	}
	if dstReplicaName == "" {
		return fmt.Errorf("failed to detach replica rebuilding src: missing required parameter dst replica name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingSrcDetach(ctx, &spdkrpc.ReplicaRebuildingSrcDetachRequest{
		Name:           srcReplicaName,
		DstReplicaName: dstReplicaName,
	})
	return errors.Wrapf(err, "failed to detach replica rebuilding src %s for rebuilding replica %s", srcReplicaName, dstReplicaName)
}

func (c *SPDKClient) ReplicaSnapshotShallowCopy(srcReplicaName, snapshotName string) error {
	if srcReplicaName == "" || snapshotName == "" {
		return fmt.Errorf("failed to finish replica rebuilding src: missing required parameter replica name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceMedTimeout)
	defer cancel()

	_, err := client.ReplicaSnapshotShallowCopy(ctx, &spdkrpc.ReplicaSnapshotShallowCopyRequest{
		Name:         srcReplicaName,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to shallow copy snapshot %s from src replica %s", snapshotName, srcReplicaName)
}

// ReplicaRebuildingDstStart asks the rebuilding replicas to expose the rebuilding lvol as a nvmf bdev if the source replica and the destination replica are on the different nodes.
// It returns an address <IP>:<Port> of the rebuilding destination bdev.
// If the source replica and the destination replicas are on the same node, the rebuilding lvol won't be exposed as a nvmf bdev and <port> will be zero.
func (c *SPDKClient) ReplicaRebuildingDstStart(replicaName string, exposeRequired bool) (dstRebuildingLvolAddress string, err error) {
	if replicaName == "" {
		return "", fmt.Errorf("failed to start replica rebuilding dst: missing required parameter replica name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.ReplicaRebuildingDstStart(ctx, &spdkrpc.ReplicaRebuildingDstStartRequest{
		Name:           replicaName,
		ExposeRequired: exposeRequired,
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to start replica rebuilding dst %s", replicaName)
	}
	return resp.Address, nil
}

func (c *SPDKClient) ReplicaRebuildingDstFinish(replicaName string, unexposeRequired bool) error {
	if replicaName == "" {
		return fmt.Errorf("failed to finish replica rebuilding dst: missing required parameter replica name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingDstFinish(ctx, &spdkrpc.ReplicaRebuildingDstFinishRequest{
		Name:             replicaName,
		UnexposeRequired: unexposeRequired,
	})
	return errors.Wrapf(err, "failed to finish replica rebuilding dst %s", replicaName)
}

func (c *SPDKClient) ReplicaRebuildingDstSnapshotCreate(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to create dst SPDK replica rebuilding snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingDstSnapshotCreate(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to create dst SPDK replica %s rebuilding snapshot %s", name, snapshotName)
}

func (c *SPDKClient) ReplicaRebuildingDstSnapshotRevert(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to revert dst SPDK replica rebuilding snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaRebuildingDstSnapshotRevert(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to revert dst SPDK replica %s rebuilding snapshot %s", name, snapshotName)
}

func (c *SPDKClient) EngineCreate(name, volumeName, frontend string, specSize uint64, replicaAddressMap map[string]string, portCount int32) (*api.Engine, error) {
	if name == "" || volumeName == "" || len(replicaAddressMap) == 0 {
		return nil, fmt.Errorf("failed to start SPDK engine: missing required parameters")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.EngineCreate(ctx, &spdkrpc.EngineCreateRequest{
		Name:              name,
		VolumeName:        volumeName,
		SpecSize:          specSize,
		ReplicaAddressMap: replicaAddressMap,
		Frontend:          frontend,
		PortCount:         portCount,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to start SPDK engine")
	}

	return api.ProtoEngineToEngine(resp), nil
}

func (c *SPDKClient) EngineDelete(name string) error {
	if name == "" {
		return fmt.Errorf("failed to delete SPDK engine: missing required parameter")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.EngineDelete(ctx, &spdkrpc.EngineDeleteRequest{
		Name: name,
	})
	return errors.Wrapf(err, "failed to delete SPDK engine %v", name)
}

func (c *SPDKClient) EngineGet(name string) (*api.Engine, error) {
	if name == "" {
		return nil, fmt.Errorf("failed to get SPDK engine: missing required parameter")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.EngineGet(ctx, &spdkrpc.EngineGetRequest{
		Name: name,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get SPDK engine %v", name)
	}
	return api.ProtoEngineToEngine(resp), nil
}

func (c *SPDKClient) EngineList() (map[string]*api.Engine, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.EngineList(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list SPDK engines")
	}

	res := map[string]*api.Engine{}
	for engineName, e := range resp.Engines {
		res[engineName] = api.ProtoEngineToEngine(e)
	}
	return res, nil
}

func (c *SPDKClient) EngineWatch(ctx context.Context) (*api.EngineStream, error) {
	client := c.getSPDKServiceClient()
	stream, err := client.EngineWatch(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open engine watch stream")
	}

	return api.NewEngineStream(stream), nil
}

func (c *SPDKClient) EngineSnapshotCreate(name, snapshotName string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("failed to create SPDK engine snapshot: missing required parameter name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.EngineSnapshotCreate(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to create SPDK engine %s snapshot %s", name, snapshotName)
	}
	return resp.SnapshotName, nil
}

func (c *SPDKClient) EngineSnapshotDelete(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to delete SPDK engine snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.EngineSnapshotDelete(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to delete SPDK engine %s snapshot %s", name, snapshotName)
}

func (c *SPDKClient) EngineSnapshotRevert(name, snapshotName string) error {
	if name == "" || snapshotName == "" {
		return fmt.Errorf("failed to revert SPDK engine snapshot: missing required parameter name or snapshot name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.EngineSnapshotRevert(ctx, &spdkrpc.SnapshotRequest{
		Name:         name,
		SnapshotName: snapshotName,
	})
	return errors.Wrapf(err, "failed to revert SPDK engine %s snapshot %s", name, snapshotName)
}

func (c *SPDKClient) EngineReplicaAdd(engineName, replicaName, replicaAddress string) error {
	if engineName == "" {
		return fmt.Errorf("failed to add replica for SPDK engine: missing required parameter engine name")
	}
	if replicaName == "" || replicaAddress == "" {
		return fmt.Errorf("failed to add replica for SPDK engine: missing required parameter replica name or address")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceLongTimeout)
	defer cancel()

	_, err := client.EngineReplicaAdd(ctx, &spdkrpc.EngineReplicaAddRequest{
		EngineName:     engineName,
		ReplicaName:    replicaName,
		ReplicaAddress: replicaAddress,
	})
	return errors.Wrapf(err, "failed to add replica %s with address %s to engine %s", replicaName, replicaAddress, engineName)
}

func (c *SPDKClient) EngineReplicaList(engineName string) (*spdkrpc.EngineReplicaListResponse, error) {
	if engineName == "" {
		return nil, fmt.Errorf("failed to list replica for SPDK engine: missing required parameter engine name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceLongTimeout)
	defer cancel()

	return client.EngineReplicaList(ctx, &spdkrpc.EngineReplicaListRequest{
		EngineName: engineName,
	})
}

func (c *SPDKClient) EngineReplicaDelete(engineName, replicaName, replicaAddress string) error {
	if engineName == "" {
		return fmt.Errorf("failed to delete replica from SPDK engine: missing required parameter engine name")
	}
	if replicaName == "" && replicaAddress == "" {
		return fmt.Errorf("failed to delete replica from SPDK engine: missing required parameter replica name or address, at least one of them is required")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.EngineReplicaDelete(ctx, &spdkrpc.EngineReplicaDeleteRequest{
		EngineName:     engineName,
		ReplicaName:    replicaName,
		ReplicaAddress: replicaAddress,
	})
	return errors.Wrapf(err, "failed to delete replica %s with address %s to engine %s", replicaName, replicaAddress, engineName)
}

func (c* SPDKClient) EngineVolumeResize(engineName string, size uint64) error {
	if engineName == "" {
		return fmt.Errorf("failed to resize volume for SPDK engine: missing required parameter engine name")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.EngineVolumeResize(ctx, &spdkrpc.EngineVolumeResizeRequest{
		EngineName: engineName,
		Size:       size,
	})
	return errors.Wrapf(err, "failed to resize volume for engine %s", engineName)
}

func (c *SPDKClient) EngineBackupCreate(req *BackupCreateRequest) (*spdkrpc.BackupCreateResponse, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.EngineBackupCreate(ctx, &spdkrpc.BackupCreateRequest{
		SnapshotName:         req.SnapshotName,
		BackupTarget:         req.BackupTarget,
		VolumeName:           req.VolumeName,
		EngineName:           req.EngineName,
		Labels:               req.Labels,
		Credential:           req.Credential,
		BackingImageName:     req.BackingImageName,
		BackingImageChecksum: req.BackingImageChecksum,
		BackupName:           req.BackupName,
		CompressionMethod:    req.CompressionMethod,
		ConcurrentLimit:      req.ConcurrentLimit,
		StorageClassName:     req.StorageClassName,
	})
}

func (c *SPDKClient) ReplicaBackupCreate(req *BackupCreateRequest) (*spdkrpc.BackupCreateResponse, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.ReplicaBackupCreate(ctx, &spdkrpc.BackupCreateRequest{
		BackupName:           req.BackupName,
		SnapshotName:         req.SnapshotName,
		BackupTarget:         req.BackupTarget,
		VolumeName:           req.VolumeName,
		ReplicaName:          req.ReplicaName,
		Size:                 int64(req.Size),
		Labels:               req.Labels,
		Credential:           req.Credential,
		BackingImageName:     req.BackingImageName,
		BackingImageChecksum: req.BackingImageChecksum,
		CompressionMethod:    req.CompressionMethod,
		ConcurrentLimit:      req.ConcurrentLimit,
		StorageClassName:     req.StorageClassName,
	})
}

func (c *SPDKClient) EngineBackupStatus(backupName, engineName, replicaAddress string) (*spdkrpc.BackupStatusResponse, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.EngineBackupStatus(ctx, &spdkrpc.BackupStatusRequest{
		Backup:         backupName,
		EngineName:     engineName,
		ReplicaAddress: replicaAddress,
	})
}

func (c *SPDKClient) ReplicaBackupStatus(backupName string) (*spdkrpc.BackupStatusResponse, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.ReplicaBackupStatus(ctx, &spdkrpc.BackupStatusRequest{
		Backup: backupName,
	})
}

func (c *SPDKClient) EngineBackupRestore(req *BackupRestoreRequest) error {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	recv, err := client.EngineBackupRestore(ctx, &spdkrpc.EngineBackupRestoreRequest{
		BackupUrl:       req.BackupUrl,
		EngineName:      req.EngineName,
		SnapshotName:    req.SnapshotName,
		Credential:      req.Credential,
		ConcurrentLimit: req.ConcurrentLimit,
	})
	if err != nil {
		return err
	}

	if len(recv.Errors) == 0 {
		return nil
	}

	taskErr := util.NewTaskError()
	for replicaAddress, replicaErr := range recv.Errors {
		replicaURL := "tcp://" + replicaAddress
		taskErr.Append(util.NewReplicaError(replicaURL, errors.New(replicaErr)))
	}

	return taskErr
}

func (c *SPDKClient) ReplicaBackupRestore(req *BackupRestoreRequest) error {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.ReplicaBackupRestore(ctx, &spdkrpc.ReplicaBackupRestoreRequest{
		BackupUrl:       req.BackupUrl,
		ReplicaName:     req.ReplicaName,
		SnapshotName:    req.SnapshotName,
		Credential:      req.Credential,
		ConcurrentLimit: req.ConcurrentLimit,
	})
	return err
}

func (c *SPDKClient) EngineBackupRestoreFinish(engineName string) error {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.EngineBackupRestoreFinish(ctx, &spdkrpc.EngineBackupRestoreFinishRequest{
		EngineName: engineName,
	})

	return err
}

func (c *SPDKClient) EngineRestoreStatus(engineName string) (*spdkrpc.RestoreStatusResponse, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.EngineRestoreStatus(ctx, &spdkrpc.RestoreStatusRequest{
		EngineName: engineName,
	})
}

func (c *SPDKClient) ReplicaRestoreStatus(replicaName string) (*spdkrpc.ReplicaRestoreStatusResponse, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.ReplicaRestoreStatus(ctx, &spdkrpc.ReplicaRestoreStatusRequest{
		ReplicaName: replicaName,
	})
}

// DiskCreate creates a disk with the given name and path.
// diskUUID is optional, if not provided, it indicates the disk is newly added.
func (c *SPDKClient) DiskCreate(diskName, diskUUID, diskPath string, blockSize int64) (*spdkrpc.Disk, error) {
	if diskName == "" || diskPath == "" {
		return nil, fmt.Errorf("failed to create disk: missing required parameters")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.DiskCreate(ctx, &spdkrpc.DiskCreateRequest{
		DiskName:  diskName,
		DiskUuid:  diskUUID,
		DiskPath:  diskPath,
		BlockSize: blockSize,
	})
}

func (c *SPDKClient) DiskGet(diskName string) (*spdkrpc.Disk, error) {
	if diskName == "" {
		return nil, fmt.Errorf("failed to get disk info: missing required parameter")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	return client.DiskGet(ctx, &spdkrpc.DiskGetRequest{
		DiskName: diskName,
	})
}

func (c *SPDKClient) DiskDelete(diskName, diskUUID string) error {
	if diskName == "" || diskUUID == "" {
		return fmt.Errorf("failed to delete disk: missing required parameters")
	}

	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.DiskDelete(ctx, &spdkrpc.DiskDeleteRequest{
		DiskName: diskName,
		DiskUuid: diskUUID,
	})
	return err
}

func (c *SPDKClient) LogSetLevel(level string) error {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.LogSetLevel(ctx, &spdkrpc.LogSetLevelRequest{
		Level: level,
	})
	return err
}

func (c *SPDKClient) LogSetFlags(flags string) error {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	_, err := client.LogSetFlags(ctx, &spdkrpc.LogSetFlagsRequest{
		Flags: flags,
	})
	return err
}

func (c *SPDKClient) LogGetLevel() (string, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.LogGetLevel(ctx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Level, nil
}

func (c *SPDKClient) LogGetFlags() (string, error) {
	client := c.getSPDKServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), GRPCServiceTimeout)
	defer cancel()

	resp, err := client.LogGetFlags(ctx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Flags, nil
}
