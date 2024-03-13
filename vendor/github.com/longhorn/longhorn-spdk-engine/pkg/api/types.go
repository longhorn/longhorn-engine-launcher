package api

import (
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/longhorn/longhorn-spdk-engine/pkg/types"
	"github.com/longhorn/longhorn-spdk-engine/proto/spdkrpc"
)

type Replica struct {
	Name       string           `json:"name"`
	LvsName    string           `json:"lvs_name"`
	LvsUUID    string           `json:"lvs_uuid"`
	SpecSize   uint64           `json:"spec_size"`
	ActualSize uint64           `json:"actual_size"`
	Head       *Lvol            `json:"head"`
	Snapshots  map[string]*Lvol `json:"snapshots"`
	IP         string           `json:"ip"`
	PortStart  int32            `json:"port_start"`
	PortEnd    int32            `json:"port_end"`
	State      string           `json:"state"`
	ErrorMsg   string           `json:"error_msg"`
	Rebuilding bool             `json:"rebuilding"`
}

type Lvol struct {
	Name         string          `json:"name"`
	UUID         string          `json:"uuid"`
	SpecSize     uint64          `json:"spec_size"`
	ActualSize   uint64          `json:"actual_size"`
	Parent       string          `json:"parent"`
	Children     map[string]bool `json:"children"`
	CreationTime string          `json:"creation_time"`
}

func ProtoLvolToLvol(l *spdkrpc.Lvol) *Lvol {
	if l == nil {
		return nil
	}
	return &Lvol{
		Name: l.Name,
		// UUID:         l.Uuid,
		SpecSize:     l.SpecSize,
		ActualSize:   l.ActualSize,
		Parent:       l.Parent,
		Children:     l.Children,
		CreationTime: l.CreationTime,
	}
}

func LvolToProtoLvol(l *Lvol) *spdkrpc.Lvol {
	if l == nil {
		return nil
	}
	return &spdkrpc.Lvol{
		Name: l.Name,
		// Uuid:         l.UUID,
		SpecSize:     l.SpecSize,
		ActualSize:   l.ActualSize,
		Parent:       l.Parent,
		Children:     l.Children,
		CreationTime: l.CreationTime,
	}
}

func ProtoReplicaToReplica(r *spdkrpc.Replica) *Replica {
	res := &Replica{
		Name:       r.Name,
		LvsName:    r.LvsName,
		LvsUUID:    r.LvsUuid,
		SpecSize:   r.SpecSize,
		ActualSize: r.ActualSize,
		Head:       ProtoLvolToLvol(r.Head),
		Snapshots:  map[string]*Lvol{},
		IP:         r.Ip,
		PortStart:  r.PortStart,
		PortEnd:    r.PortEnd,
		State:      r.State,
		ErrorMsg:   r.ErrorMsg,
		Rebuilding: r.Rebuilding,
	}
	for snapName, snapProtoLvol := range r.Snapshots {
		res.Snapshots[snapName] = ProtoLvolToLvol(snapProtoLvol)
	}

	return res
}

func ReplicaToProtoReplica(r *Replica) *spdkrpc.Replica {
	snapshots := map[string]*spdkrpc.Lvol{}
	for name, snapshot := range r.Snapshots {
		snapshots[name] = LvolToProtoLvol(snapshot)
	}

	return &spdkrpc.Replica{
		Name:       r.Name,
		LvsName:    r.LvsName,
		LvsUuid:    r.LvsUUID,
		SpecSize:   r.SpecSize,
		ActualSize: r.ActualSize,
		Ip:         r.IP,
		PortStart:  r.PortStart,
		PortEnd:    r.PortEnd,
		Head:       LvolToProtoLvol(r.Head),
		Snapshots:  snapshots,
		Rebuilding: r.Rebuilding,
		State:      r.State,
		ErrorMsg:   r.ErrorMsg,
	}
}

type Engine struct {
	Name              string                `json:"name"`
	VolumeName        string                `json:"volumeName"`
	SpecSize          uint64                `json:"spec_size"`
	ActualSize        uint64                `json:"actual_size"`
	IP                string                `json:"ip"`
	Port              int32                 `json:"port"`
	ReplicaAddressMap map[string]string     `json:"replica_address_map"`
	ReplicaModeMap    map[string]types.Mode `json:"replica_mode_map"`
	Head              *Lvol                 `json:"head"`
	Snapshots         map[string]*Lvol      `json:"snapshots"`
	Frontend          string                `json:"frontend"`
	Endpoint          string                `json:"endpoint"`
	State             string                `json:"state"`
	ErrorMsg          string                `json:"error_msg"`
}

func ProtoEngineToEngine(e *spdkrpc.Engine) *Engine {
	res := &Engine{
		Name:              e.Name,
		VolumeName:        e.VolumeName,
		SpecSize:          e.SpecSize,
		ActualSize:        e.ActualSize,
		IP:                e.Ip,
		Port:              e.Port,
		ReplicaAddressMap: e.ReplicaAddressMap,
		ReplicaModeMap:    map[string]types.Mode{},
		Head:              ProtoLvolToLvol(e.Head),
		Snapshots:         map[string]*Lvol{},
		Frontend:          e.Frontend,
		Endpoint:          e.Endpoint,
		State:             e.State,
		ErrorMsg:          e.ErrorMsg,
	}
	for rName, mode := range e.ReplicaModeMap {
		res.ReplicaModeMap[rName] = spdkrpc.GRPCReplicaModeToReplicaMode(mode)
	}
	for snapshotName, snapProtoLvol := range e.Snapshots {
		res.Snapshots[snapshotName] = ProtoLvolToLvol(snapProtoLvol)
	}

	return res
}

type DiskInfo struct {
	ID          string
	UUID        string
	Path        string
	Type        string
	TotalSize   int64
	FreeSize    int64
	TotalBlocks int64
	FreeBlocks  int64
	BlockSize   int64
	ClusterSize int64
}

type ReplicaStream struct {
	stream spdkrpc.SPDKService_ReplicaWatchClient
}

func NewReplicaStream(stream spdkrpc.SPDKService_ReplicaWatchClient) *ReplicaStream {
	return &ReplicaStream{
		stream,
	}
}

func (s *ReplicaStream) Recv() (*emptypb.Empty, error) {
	return s.stream.Recv()
}

type EngineStream struct {
	stream spdkrpc.SPDKService_EngineWatchClient
}

func NewEngineStream(stream spdkrpc.SPDKService_EngineWatchClient) *EngineStream {
	return &EngineStream{
		stream,
	}
}

func (s *EngineStream) Recv() (*emptypb.Empty, error) {
	return s.stream.Recv()
}
