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
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Infof("Snapshotting volume: snapshot %v", req.SnapshotVolume.Name)

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.volumeSnapshot(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkVolumeSnapshot(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) volumeSnapshot(ctx context.Context, req *rpc.EngineVolumeSnapshotRequest) (resp *rpc.EngineVolumeSnapshotProxyResponse, err error) {
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

func (p *Proxy) spdkVolumeSnapshot(ctx context.Context, req *rpc.EngineVolumeSnapshotRequest) (resp *rpc.EngineVolumeSnapshotProxyResponse, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotList(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotListProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.Address,
		"engineName":         req.EngineName,
		"volumeName":         req.VolumeName,
		"backendStoreDriver": req.BackendStoreDriver,
	})
	log.Trace("Listing snapshots")

	switch req.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotList(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotList(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotList(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotListProxyResponse, err error) {
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

func (p *Proxy) spdkSnapshotList(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotListProxyResponse, err error) {
	/* TODO: implement this */
	return &rpc.EngineSnapshotListProxyResponse{
		Disks: map[string]*rpc.EngineSnapshotDiskInfo{},
	}, nil
}

func (p *Proxy) SnapshotClone(ctx context.Context, req *rpc.EngineSnapshotCloneRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Infof("Cloning snapshot from %v to %v", req.FromEngineAddress, req.ProxyEngineRequest.Address)

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotClone(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotClone(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotClone(ctx context.Context, req *rpc.EngineSnapshotCloneRequest) (resp *emptypb.Empty, err error) {
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

func (p *Proxy) spdkSnapshotClone(ctx context.Context, req *rpc.EngineSnapshotCloneRequest) (resp *emptypb.Empty, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotCloneStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotCloneStatusProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.Address,
		"engineName":         req.EngineName,
		"volumeName":         req.VolumeName,
		"backendStoreDriver": req.BackendStoreDriver,
	})
	log.Trace("Getting snapshot clone status")

	switch req.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotCloneStatus(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotCloneStatus(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotCloneStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotCloneStatusProxyResponse, err error) {
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

func (p *Proxy) spdkSnapshotCloneStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotCloneStatusProxyResponse, err error) {
	/* TODO: implement this */
	return &rpc.EngineSnapshotCloneStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotCloneStatusResponse{},
	}, nil
}

func (p *Proxy) SnapshotRevert(ctx context.Context, req *rpc.EngineSnapshotRevertRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Infof("Reverting snapshot %v", req.Name)

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotRevert(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotRevert(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotRevert(ctx context.Context, req *rpc.EngineSnapshotRevertRequest) (resp *emptypb.Empty, err error) {
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

func (p *Proxy) spdkSnapshotRevert(ctx context.Context, req *rpc.EngineSnapshotRevertRequest) (resp *emptypb.Empty, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotPurge(ctx context.Context, req *rpc.EngineSnapshotPurgeRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Info("Purging snapshots")

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotPurge(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotPurge(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotPurge(ctx context.Context, req *rpc.EngineSnapshotPurgeRequest) (resp *emptypb.Empty, err error) {
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

func (p *Proxy) spdkSnapshotPurge(ctx context.Context, req *rpc.EngineSnapshotPurgeRequest) (resp *emptypb.Empty, err error) {
	/* TODO: implement this */
	return &emptypb.Empty{}, nil
}

func (p *Proxy) SnapshotPurgeStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotPurgeStatusProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.Address,
		"engineName":         req.EngineName,
		"volumeName":         req.VolumeName,
		"backendStoreDriver": req.BackendStoreDriver,
	})
	log.Trace("Getting snapshot purge status")

	switch req.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotPurgeStatus(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotPurgeStatus(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotPurgeStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotPurgeStatusProxyResponse, err error) {
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

func (p *Proxy) spdkSnapshotPurgeStatus(ctx context.Context, req *rpc.ProxyEngineRequest) (resp *rpc.EngineSnapshotPurgeStatusProxyResponse, err error) {
	/* TODO: implement this */
	return &rpc.EngineSnapshotPurgeStatusProxyResponse{
		Status: map[string]*eptypes.SnapshotPurgeStatusResponse{},
	}, nil
}

func (p *Proxy) SnapshotRemove(ctx context.Context, req *rpc.EngineSnapshotRemoveRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Infof("Removing snapshots %v", req.Names)

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotRemove(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotRemove(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotRemove(ctx context.Context, req *rpc.EngineSnapshotRemoveRequest) (resp *emptypb.Empty, err error) {
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

func (p *Proxy) spdkSnapshotRemove(ctx context.Context, req *rpc.EngineSnapshotRemoveRequest) (resp *emptypb.Empty, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotHash(ctx context.Context, req *rpc.EngineSnapshotHashRequest) (resp *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Infof("Hashing snapshot %v with rehash %v", req.SnapshotName, req.Rehash)

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotHash(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotHash(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotHash(ctx context.Context, req *rpc.EngineSnapshotHashRequest) (resp *emptypb.Empty, err error) {
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

func (p *Proxy) spdkSnapshotHash(ctx context.Context, req *rpc.EngineSnapshotHashRequest) (resp *emptypb.Empty, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}

func (p *Proxy) SnapshotHashStatus(ctx context.Context, req *rpc.EngineSnapshotHashStatusRequest) (resp *rpc.EngineSnapshotHashStatusProxyResponse, err error) {
	log := logrus.WithFields(logrus.Fields{
		"serviceURL":         req.ProxyEngineRequest.Address,
		"engineName":         req.ProxyEngineRequest.EngineName,
		"volumeName":         req.ProxyEngineRequest.VolumeName,
		"backendStoreDriver": req.ProxyEngineRequest.BackendStoreDriver,
	})
	log.Trace("Getting snapshot hash status")

	switch req.ProxyEngineRequest.BackendStoreDriver {
	case rpc.BackendStoreDriver_v1:
		return p.snapshotHashStatus(ctx, req)
	case rpc.BackendStoreDriver_v2:
		return p.spdkSnapshotHashStatus(ctx, req)
	default:
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "unknown backend store driver %v", req.ProxyEngineRequest.BackendStoreDriver)
	}
}

func (p *Proxy) snapshotHashStatus(ctx context.Context, req *rpc.EngineSnapshotHashStatusRequest) (resp *rpc.EngineSnapshotHashStatusProxyResponse, err error) {
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

func (p *Proxy) spdkSnapshotHashStatus(ctx context.Context, req *rpc.EngineSnapshotHashStatusRequest) (resp *rpc.EngineSnapshotHashStatusProxyResponse, err error) {
	return nil, grpcstatus.Errorf(grpccodes.Unimplemented, "not implemented")
}
