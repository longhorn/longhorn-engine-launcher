package proxy

import (
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	eclient "github.com/longhorn/longhorn-engine/pkg/controller/client"
	esync "github.com/longhorn/longhorn-engine/pkg/sync"
	eptypes "github.com/longhorn/longhorn-engine/proto/ptypes"

	rpc "github.com/longhorn/longhorn-instance-manager/pkg/imrpc"
)

func (p *Proxy) VolumeSnapshot(ctx context.Context, req *rpc.EngineVolumeSnapshotRequest) (resp *rpc.EngineVolumeSnapshotProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Infof("Snapshotting volume: snapshot %v", req.SnapshotVolume.Name)

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.VolumeSnapshot(ctx, req)
}

func (ops V1DataEngineProxyOps) VolumeSnapshot(ctx context.Context, req *rpc.EngineVolumeSnapshotRequest) (resp *rpc.EngineVolumeSnapshotProxyResponse, err error) {
	c, err := eclient.NewControllerClient(req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	recv, err := c.VolumeSnapshot(req.SnapshotVolume.Name, req.SnapshotVolume.Labels)
	if err != nil {
		return nil, err
	}

	return &rpc.EngineVolumeSnapshotProxyResponse{
		Snapshot: &eptypes.VolumeSnapshotReply{
			Name: recv,
		},
	}, nil
}

func (ops V2DataEngineProxyOps) VolumeSnapshot(ctx context.Context, req *rpc.EngineVolumeSnapshotRequest) (resp *rpc.EngineVolumeSnapshotProxyResponse, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotList(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotListProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.Address,
		"engineName": req.EngineName,
		"volumeName": req.VolumeName,
		"dataEngine": req.DataEngine,
	})
	log.Trace("Listing snapshots")

	op, ok := p.ops[req.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.DataEngine)
	}
	return op.SnapshotList(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotList(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotListProxyResponse, err error) {
	c, err := eclient.NewControllerClient(req.Address, req.VolumeName, req.EngineName)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	recv, err := c.ReplicaList()
	if err != nil {
		return nil, err
	}

	snapshotsDiskInfo, err := esync.GetSnapshotsInfo(recv, req.VolumeName)
	if err != nil {
		return nil, err
	}

	resp = &rpc.EngineSnapshotListProxyResponse{
		Disks: map[string]*rpc.EngineSnapshotDiskInfo{},
	}
	for k, v := range snapshotsDiskInfo {
		resp.Disks[k] = &rpc.EngineSnapshotDiskInfo{
			Name:        v.Name,
			Parent:      v.Parent,
			Children:    v.Children,
			Removed:     v.Removed,
			UserCreated: v.UserCreated,
			Created:     v.Created,
			Size:        v.Size,
			Labels:      v.Labels,
		}
	}

	return resp, nil
}

func (ops V2DataEngineProxyOps) SnapshotList(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotListProxyResponse, err error) {
	/* TODO: implement this */
	return &rpc.EngineSnapshotListProxyResponse{
		Disks: map[string]*rpc.EngineSnapshotDiskInfo{},
	}, nil
}

func (p *Proxy) SnapshotClone(ctx context.Context, req *rpc.EngineSnapshotCloneRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Infof("Cloning snapshot from %v to %v", req.FromEngineAddress, req.ProxyEngineRequest.Address)

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.SnapshotClone(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotClone(ctx context.Context, req *rpc.EngineSnapshotCloneRequest) (resp *emptypb.Empty, err error) {
	cFrom, err := eclient.NewControllerClient(req.FromEngineAddress, req.FromVolumeName, req.FromEngineName)
	if err != nil {
		return nil, err
	}
	defer cFrom.Close()

	cTo, err := eclient.NewControllerClient(req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}
	defer cTo.Close()

	err = esync.CloneSnapshot(cTo, cFrom, req.ProxyEngineRequest.VolumeName, req.FromVolumeName, req.SnapshotName,
		req.ExportBackingImageIfExist, int(req.FileSyncHttpClientTimeout))
	if err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (ops V2DataEngineProxyOps) SnapshotClone(ctx context.Context, req *rpc.EngineSnapshotCloneRequest) (resp *emptypb.Empty, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotCloneStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotCloneStatusProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.Address,
		"engineName": req.EngineName,
		"volumeName": req.VolumeName,
		"dataEngine": req.DataEngine,
	})
	log.Trace("Getting snapshot clone status")

	op, ok := p.ops[req.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.DataEngine)
	}
	return op.SnapshotCloneStatus(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotCloneStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotCloneStatusProxyResponse, err error) {
	c, err := eclient.NewControllerClient(req.Address, req.VolumeName, req.EngineName)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	recv, err := esync.CloneStatus(c, req.VolumeName)
	if err != nil {
		return nil, err
	}

	resp = &rpc.EngineSnapshotCloneStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotCloneStatusResponse{},
	}
	for k, v := range recv {
		resp.Status[k] = &eptypes.SnapshotCloneStatusResponse{
			IsCloning:          v.IsCloning,
			Error:              v.Error,
			Progress:           int32(v.Progress),
			State:              v.State,
			FromReplicaAddress: v.FromReplicaAddress,
			SnapshotName:       v.SnapshotName,
		}
	}

	return resp, nil
}

func (ops V2DataEngineProxyOps) SnapshotCloneStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotCloneStatusProxyResponse, err error) {
	/* TODO: implement this */
	return &rpc.EngineSnapshotCloneStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotCloneStatusResponse{},
	}, nil
}

func (p *Proxy) SnapshotRevert(ctx context.Context, req *rpc.EngineSnapshotRevertRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Infof("Reverting snapshot %v", req.Name)

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.SnapshotRevert(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotRevert(ctx context.Context, req *rpc.EngineSnapshotRevertRequest) (resp *emptypb.Empty, err error) {
	c, err := eclient.NewControllerClient(req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	if err := c.VolumeRevert(req.Name); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (ops V2DataEngineProxyOps) SnapshotRevert(ctx context.Context, req *rpc.EngineSnapshotRevertRequest) (resp *emptypb.Empty, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotPurge(ctx context.Context, req *rpc.EngineSnapshotPurgeRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Info("Purging snapshots")

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.SnapshotPurge(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotPurge(ctx context.Context, req *rpc.EngineSnapshotPurgeRequest) (resp *emptypb.Empty, err error) {
	task, err := esync.NewTask(ctx, req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}

	if err := task.PurgeSnapshots(req.SkipIfInProgress); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (ops V2DataEngineProxyOps) SnapshotPurge(ctx context.Context, req *rpc.EngineSnapshotPurgeRequest) (resp *emptypb.Empty, err error) {
	/* TODO: implement this */
	return &emptypb.Empty{}, nil
}

func (p *Proxy) SnapshotPurgeStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotPurgeStatusProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.Address,
		"engineName": req.EngineName,
		"volumeName": req.VolumeName,
		"dataEngine": req.DataEngine,
	})
	log.Trace("Getting snapshot purge status")

	op, ok := p.ops[req.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.DataEngine)
	}
	return op.SnapshotPurgeStatus(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotPurgeStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotPurgeStatusProxyResponse, err error) {
	task, err := esync.NewTask(ctx, req.Address, req.VolumeName, req.EngineName)
	if err != nil {
		return nil, err
	}

	recv, err := task.PurgeSnapshotStatus()
	if err != nil {
		return nil, err
	}

	resp = &rpc.EngineSnapshotPurgeStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotPurgeStatusResponse{},
	}
	for k, v := range recv {
		resp.Status[k] = &eptypes.SnapshotPurgeStatusResponse{
			IsPurging: v.IsPurging,
			Error:     v.Error,
			Progress:  int32(v.Progress),
			State:     v.State,
		}
	}

	return resp, nil
}

func (ops V2DataEngineProxyOps) SnapshotPurgeStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotPurgeStatusProxyResponse, err error) {
	/* TODO: implement this */
	return &rpc.EngineSnapshotPurgeStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotPurgeStatusResponse{},
	}, nil
}

func (p *Proxy) SnapshotRemove(ctx context.Context, req *rpc.EngineSnapshotRemoveRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Infof("Removing snapshots %v", req.Names)

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.SnapshotRemove(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotRemove(ctx context.Context, req *rpc.EngineSnapshotRemoveRequest) (resp *emptypb.Empty, err error) {
	task, err := esync.NewTask(ctx, req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, name := range req.Names {
		if err := task.DeleteSnapshot(name); err != nil {
			lastErr = err
			logrus.WithError(err).Warnf("Failed to delete %s", name)
		}
	}

	return &emptypb.Empty{}, lastErr
}

func (ops V2DataEngineProxyOps) SnapshotRemove(ctx context.Context, req *rpc.EngineSnapshotRemoveRequest) (resp *emptypb.Empty, err error) {
	/* TODO: implement this */
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotHash(ctx context.Context, req *rpc.EngineSnapshotHashRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Infof("Hashing snapshot %v with rehash %v", req.SnapshotName, req.Rehash)

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.SnapshotHash(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotHash(ctx context.Context, req *rpc.EngineSnapshotHashRequest) (resp *emptypb.Empty, err error) {
	task, err := esync.NewTask(ctx, req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}

	if err := task.HashSnapshot(req.SnapshotName, req.Rehash); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (ops V2DataEngineProxyOps) SnapshotHash(ctx context.Context, req *rpc.EngineSnapshotHashRequest) (resp *emptypb.Empty, err error) {
	/* TODO: implement this */
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotHashStatus(ctx context.Context, req *rpc.EngineSnapshotHashStatusRequest) (resp *rpc.EngineSnapshotHashStatusProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL": req.ProxyEngineRequest.Address,
		"engineName": req.ProxyEngineRequest.EngineName,
		"volumeName": req.ProxyEngineRequest.VolumeName,
		"dataEngine": req.ProxyEngineRequest.DataEngine,
	})
	log.Trace("Getting snapshot hash status")

	op, ok := p.ops[req.ProxyEngineRequest.DataEngine]
	if !ok {
		return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "unsupported data engine %v", req.ProxyEngineRequest.DataEngine)
	}
	return op.SnapshotHashStatus(ctx, req)
}

func (ops V1DataEngineProxyOps) SnapshotHashStatus(ctx context.Context, req *rpc.EngineSnapshotHashStatusRequest) (resp *rpc.EngineSnapshotHashStatusProxyResponse, err error) {
	task, err := esync.NewTask(ctx, req.ProxyEngineRequest.Address, req.ProxyEngineRequest.VolumeName,
		req.ProxyEngineRequest.EngineName)
	if err != nil {
		return nil, err
	}

	recv, err := task.HashSnapshotStatus(req.SnapshotName)
	if err != nil {
		return nil, err
	}

	resp = &rpc.EngineSnapshotHashStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotHashStatusResponse{},
	}
	for k, v := range recv {
		resp.Status[k] = &eptypes.SnapshotHashStatusResponse{
			State:             v.State,
			Checksum:          v.Checksum,
			Error:             v.Error,
			SilentlyCorrupted: v.SilentlyCorrupted,
		}
	}

	return resp, nil
}

func (ops V2DataEngineProxyOps) SnapshotHashStatus(ctx context.Context, req *rpc.EngineSnapshotHashStatusRequest) (resp *rpc.EngineSnapshotHashStatusProxyResponse, err error) {
	/* TODO: implement this */
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}
