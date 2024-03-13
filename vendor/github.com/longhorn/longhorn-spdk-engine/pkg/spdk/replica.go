package spdk

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/longhorn/backupstore"
	btypes "github.com/longhorn/backupstore/types"
	butil "github.com/longhorn/backupstore/util"
	commonNet "github.com/longhorn/go-common-libs/net"
	"github.com/longhorn/go-spdk-helper/pkg/jsonrpc"
	spdkclient "github.com/longhorn/go-spdk-helper/pkg/spdk/client"
	spdktypes "github.com/longhorn/go-spdk-helper/pkg/spdk/types"
	helpertypes "github.com/longhorn/go-spdk-helper/pkg/types"
	helperutil "github.com/longhorn/go-spdk-helper/pkg/util"

	"github.com/longhorn/longhorn-spdk-engine/pkg/types"
	"github.com/longhorn/longhorn-spdk-engine/pkg/util"
	"github.com/longhorn/longhorn-spdk-engine/proto/spdkrpc"
)

const (
	restorePeriodicRefreshInterval = 2 * time.Second
)

type Replica struct {
	sync.RWMutex

	ctx context.Context

	// ActiveChain stores the backing image info in index 0.
	// If a replica does not contain a backing image, the first entry will be nil.
	// The last entry of the chain is always the head lvol.
	ActiveChain []*Lvol
	ChainLength int
	// SnapshotLvolMap map[<snapshot lvol name>]. <snapshot lvol name> consists of `<replica name>-snap-<snapshot name>`
	SnapshotLvolMap map[string]*Lvol

	Name       string
	Alias      string
	LvsName    string
	LvsUUID    string
	SpecSize   uint64
	ActualSize uint64
	IP         string
	PortStart  int32
	PortEnd    int32

	State    types.InstanceState
	ErrorMsg string

	IsExposeRequired bool
	IsExposed        bool

	isRebuilding   bool
	rebuildingLvol *Lvol
	rebuildingPort int32

	rebuildingDstReplicaName string
	rebuildingDstBdevName    string
	rebuildingDstBdevType    spdktypes.BdevType

	isRestoring bool
	restore     *Restore

	portAllocator *util.Bitmap
	// UpdateCh should not be protected by the replica lock
	UpdateCh chan interface{}

	log logrus.FieldLogger

	// TODO: Record error message
}

type Lvol struct {
	Name       string
	UUID       string
	Alias      string
	SpecSize   uint64
	ActualSize uint64
	Parent     string
	// Children is map[<snapshot lvol name>] rather than map[<snapshot name>]. <snapshot lvol name> consists of `<replica name>-snap-<snapshot name>`
	Children     map[string]*Lvol
	CreationTime string
}

func ServiceReplicaToProtoReplica(r *Replica) *spdkrpc.Replica {
	res := &spdkrpc.Replica{
		Name:      r.Name,
		LvsName:   r.LvsName,
		LvsUuid:   r.LvsUUID,
		SpecSize:  r.SpecSize,
		Snapshots: map[string]*spdkrpc.Lvol{},
		Ip:        r.IP,
		PortStart: r.PortStart,
		PortEnd:   r.PortEnd,
		State:     string(r.State),
		ErrorMsg:  r.ErrorMsg,
	}

	res.Head = ServiceLvolToProtoLvol(r.Name, r.ActiveChain[r.ChainLength-1])
	// spdkrpc.Replica.Snapshots is map[<snapshot name>] rather than map[<snapshot lvol name>]
	for lvolName, lvol := range r.SnapshotLvolMap {
		res.Snapshots[GetSnapshotNameFromReplicaSnapshotLvolName(r.Name, lvolName)] = ServiceLvolToProtoLvol(r.Name, lvol)
	}

	return res
}

func ServiceLvolToProtoLvol(replicaName string, lvol *Lvol) *spdkrpc.Lvol {
	res := &spdkrpc.Lvol{
		Uuid:         lvol.UUID,
		SpecSize:     lvol.SpecSize,
		ActualSize:   lvol.ActualSize,
		Parent:       GetSnapshotNameFromReplicaSnapshotLvolName(replicaName, lvol.Parent),
		Children:     map[string]bool{},
		CreationTime: lvol.CreationTime,
	}

	if lvol.Name == replicaName {
		res.Name = types.VolumeHead
	} else {
		res.Name = GetSnapshotNameFromReplicaSnapshotLvolName(replicaName, lvol.Name)
	}

	for childLvolName := range lvol.Children {
		// spdkrpc.Lvol.Children is map[<snapshot name>] rather than map[<snapshot lvol name>]
		if childLvolName == replicaName {
			res.Children[types.VolumeHead] = true
		} else {
			res.Children[GetSnapshotNameFromReplicaSnapshotLvolName(replicaName, childLvolName)] = true
		}
	}

	return res
}

func BdevLvolInfoToServiceLvol(bdev *spdktypes.BdevInfo) *Lvol {
	return &Lvol{
		Name:       spdktypes.GetLvolNameFromAlias(bdev.Aliases[0]),
		Alias:      bdev.Aliases[0],
		UUID:       bdev.UUID,
		SpecSize:   bdev.NumBlocks * uint64(bdev.BlockSize),
		ActualSize: bdev.DriverSpecific.Lvol.NumAllocatedClusters * defaultClusterSize,
		Parent:     bdev.DriverSpecific.Lvol.BaseSnapshot,
		// Need to update this separately
		Children:     map[string]*Lvol{},
		CreationTime: bdev.CreationTime,
	}
}

func NewReplica(ctx context.Context, replicaName, lvsName, lvsUUID string, specSize, actualSize uint64, updateCh chan interface{}) *Replica {
	log := logrus.StandardLogger().WithFields(logrus.Fields{
		"replicaName": replicaName,
		"lvsName":     lvsName,
		"lvsUUID":     lvsUUID,
	})

	roundedSpecSize := util.RoundUp(specSize, helpertypes.MiB)
	if roundedSpecSize != specSize {
		log.Infof("Rounded up spec size from %v to %v since the specSize should be multiple of MiB", specSize, roundedSpecSize)
	}
	log.WithField("specSize", roundedSpecSize)

	return &Replica{
		ctx: ctx,

		ActiveChain: []*Lvol{
			nil,
			{
				Name:       replicaName,
				Alias:      spdktypes.GetLvolAlias(lvsName, replicaName),
				SpecSize:   roundedSpecSize,
				ActualSize: actualSize,
				Children:   map[string]*Lvol{},
			},
		},
		ChainLength:     2,
		SnapshotLvolMap: map[string]*Lvol{},
		Name:            replicaName,
		Alias:           spdktypes.GetLvolAlias(lvsName, replicaName),
		LvsName:         lvsName,
		LvsUUID:         lvsUUID,
		SpecSize:        roundedSpecSize,
		State:           types.InstanceStatePending,

		restore: &Restore{},

		UpdateCh: updateCh,

		log: log,
	}
}

func (r *Replica) Sync(spdkClient *spdkclient.Client) (err error) {
	r.Lock()
	defer r.Unlock()
	// It's better to let the server send the update signal

	// This lvol and nvmf subsystem fetch should be protected by replica lock, in case of snapshot operations happened during the sync-up.
	bdevLvolMap, err := GetBdevLvolMap(spdkClient)
	if err != nil {
		return err
	}

	if r.State == types.InstanceStatePending {
		return r.construct(bdevLvolMap)
	}

	subsystemMap, err := GetNvmfSubsystemMap(spdkClient)
	if err != nil {
		return err
	}

	return r.validateAndUpdate(bdevLvolMap, subsystemMap)
}

// construct build Replica with the SnapshotLvolMap and SnapshotChain from the bdev lvol list.
// This function is typically invoked for the existing lvols after node/service restart and device add.
func (r *Replica) construct(bdevLvolMap map[string]*spdktypes.BdevInfo) (err error) {
	defer func() {
		if err != nil {
			r.State = types.InstanceStateError
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	switch r.State {
	case types.InstanceStatePending:
		break
	case types.InstanceStateRunning:
		if r.isRebuilding {
			break
		}
		fallthrough
	default:
		return fmt.Errorf("invalid state %s for replica %s construct", r.Name, r.State)
	}

	if err := r.validateReplicaInfo(bdevLvolMap[r.Name]); err != nil {
		return err
	}

	newSnapshotLvolMap, err := constructSnapshotLvolMap(r.Name, bdevLvolMap)
	if err != nil {
		return err
	}
	newChain, err := constructActiveChainFromSnapshotLvolMap(r.Name, newSnapshotLvolMap, bdevLvolMap)
	if err != nil {
		return err
	}

	r.ActiveChain = newChain
	r.ChainLength = len(r.ActiveChain)
	r.SnapshotLvolMap = newSnapshotLvolMap
	if r.State == types.InstanceStatePending {
		r.State = types.InstanceStateStopped
	}

	return nil
}

func (r *Replica) validateAndUpdate(bdevLvolMap map[string]*spdktypes.BdevInfo, subsystemMap map[string]*spdktypes.NvmfSubsystem) (err error) {
	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				r.log.WithError(err).Error("Found error during validation and update")
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	// Stop syncing with the SPDK TGT server if the replica does not contain any valid SPDK components.
	if r.State != types.InstanceStateRunning && r.State != types.InstanceStateStopped {
		return nil
	}

	// Should not sync a rebuilding replica since the snapshot map as well as the active chain is not ready.
	if r.rebuildingLvol != nil {
		return nil
	}

	if err := r.validateReplicaInfo(bdevLvolMap[r.Name]); err != nil {
		return err
	}

	newSnapshotLvolMap, err := constructSnapshotLvolMap(r.Name, bdevLvolMap)
	if err != nil {
		return err
	}
	if len(r.SnapshotLvolMap) != len(newSnapshotLvolMap) {
		return fmt.Errorf("replica current active snapshot lvol map length %d is not the same as the latest snapshot lvol map length %d", len(r.SnapshotLvolMap), len(newSnapshotLvolMap))
	}
	for snapshotLvolName := range r.SnapshotLvolMap {
		if err := compareSvcLvols(r.SnapshotLvolMap[snapshotLvolName], newSnapshotLvolMap[snapshotLvolName], true, true); err != nil {
			return err
		}
	}

	newChain, err := constructActiveChainFromSnapshotLvolMap(r.Name, newSnapshotLvolMap, bdevLvolMap)
	if err != nil {
		return err
	}

	if len(r.ActiveChain) != len(newChain) {
		return fmt.Errorf("replica current active chain length %d is not the same as the latest chain length %d", len(r.ActiveChain), len(newChain))
	}

	for idx, svcLvol := range r.ActiveChain {
		newSvcLvol := newChain[idx]
		// Handle nil backing image separately
		if idx == 0 {
			if svcLvol == nil && newSvcLvol == nil {
				continue
			}
			if svcLvol != nil && newSvcLvol == nil {
				return fmt.Errorf("replica current backing image is %v while the latest chain contains a nil backing image", svcLvol.Name)
			}
			if svcLvol == nil && newSvcLvol != nil {
				return fmt.Errorf("replica current backing image is nil while the latest chain contains backing image %v", newSvcLvol.Name)
			}
		}

		if err := compareSvcLvols(svcLvol, newSvcLvol, true, svcLvol.Name != r.Name); err != nil {
			return err
		}
		// Then update the actual size for the head lvol
		if svcLvol.Name == r.Name {
			svcLvol.ActualSize = newSvcLvol.ActualSize
		}
	}

	replicaActualSize := newChain[r.ChainLength-1].ActualSize
	for _, snapLvol := range newSnapshotLvolMap {
		replicaActualSize += snapLvol.ActualSize
	}
	r.ActualSize = replicaActualSize

	if r.State == types.InstanceStateRunning {
		if r.IP == "" {
			return fmt.Errorf("found invalid IP %s for replica %s", r.IP, r.Name)
		}
		if r.PortStart == 0 || r.PortEnd == 0 || r.PortStart > r.PortEnd {
			return fmt.Errorf("found invalid Ports [%d, %d] for the running replica %s", r.PortStart, r.PortEnd, r.Name)
		}
	}

	// In case of a stopped replica being wrongly exposed, this function will check the exposing state anyway.
	if r.isRestoring {
		r.log.Info("Replica is being restored, skip the exposing state check")
		return nil
	}

	nqn := helpertypes.GetNQN(r.Name)
	exposedPort, exposedPortErr := getExposedPort(subsystemMap[nqn])
	if r.IsExposed {
		if exposedPortErr != nil {
			return errors.Wrapf(err, "failed to find the actual port in subsystem NQN %s for replica %s, which should be exposed at %d", nqn, r.Name, r.PortStart)
		}
		if exposedPort != r.PortStart {
			return fmt.Errorf("found mismatching between the actual exposed port %d and the recorded port %d for exposed replica %s", exposedPort, r.PortStart, r.Name)
		}
	} else {
		if exposedPortErr == nil {
			return fmt.Errorf("found the actual port %d in subsystem NQN %s for replica %s, which should not be exposed", exposedPort, nqn, r.Name)
		}
	}

	return nil
}

func compareSvcLvols(prev, cur *Lvol, checkChildren, checkActualSize bool) error {
	if prev == nil && cur == nil {
		return nil
	}
	if prev == nil {
		return fmt.Errorf("cannot find the corresponding prev lvol")
	}
	if cur == nil {
		return fmt.Errorf("cannot find the corresponding cur lvol")
	}
	if prev.Name != cur.Name || prev.UUID != cur.UUID || prev.CreationTime != cur.CreationTime || prev.SpecSize != cur.SpecSize || prev.Parent != cur.Parent || len(prev.Children) != len(cur.Children) {
		return fmt.Errorf("found mismatching lvol %+v with recorded prev lvol %+v", cur, prev)
	}
	if checkChildren {
		for childName := range prev.Children {
			if cur.Children[childName] == nil {
				return fmt.Errorf("found mismatching lvol children %+v with recorded prev lvol children %+v when validating lvol %s", cur.Children, prev.Children, prev.Name)
			}
		}
	}

	// TODO:
	// When deleting a snapshot lvol, the merge of lvols results in a change of actual size. Do not return error to prevent a false alarm.
	// Need to revisit the actual size check.
	if checkActualSize && prev.ActualSize != cur.ActualSize {
		logrus.Warnf("Found mismatching lvol actual size %v with recorded prev lvol actual size %v when validating lvol %s", cur.ActualSize, prev.ActualSize, prev.Name)
	}

	return nil
}

func getExposedPort(subsystem *spdktypes.NvmfSubsystem) (exposedPort int32, err error) {
	if subsystem == nil || len(subsystem.ListenAddresses) == 0 {
		return 0, fmt.Errorf("cannot find the Nvmf subsystem")
	}

	port := 0
	for _, listenAddr := range subsystem.ListenAddresses {
		if !strings.EqualFold(string(listenAddr.Adrfam), string(spdktypes.NvmeAddressFamilyIPv4)) ||
			!strings.EqualFold(string(listenAddr.Trtype), string(spdktypes.NvmeTransportTypeTCP)) {
			continue
		}
		port, err = strconv.Atoi(listenAddr.Trsvcid)
		if err != nil {
			return 0, err
		}
		return int32(port), nil
	}

	return 0, fmt.Errorf("cannot find a exposed port in the Nvmf subsystem")
}

func (r *Replica) validateReplicaInfo(headBdevLvol *spdktypes.BdevInfo) (err error) {
	if headBdevLvol == nil {
		return fmt.Errorf("found nil head bdev lvol for replica %s", r.Name)
	}
	if headBdevLvol.DriverSpecific.Lvol.Snapshot {
		return fmt.Errorf("found the head bdev lvol is a snapshot lvol for replica %s", r.Name)
	}
	if r.LvsUUID != headBdevLvol.DriverSpecific.Lvol.LvolStoreUUID {
		return fmt.Errorf("found mismatching lvol LvsUUID %v with recorded LvsUUID %v for replica %s", headBdevLvol.DriverSpecific.Lvol.LvolStoreUUID, r.LvsUUID, r.Name)
	}
	bdevLvolSpecSize := headBdevLvol.NumBlocks * uint64(headBdevLvol.BlockSize)
	if r.SpecSize != 0 && r.SpecSize != bdevLvolSpecSize {
		return fmt.Errorf("found mismatching lvol spec size %v with recorded spec size %v for replica %s", bdevLvolSpecSize, r.SpecSize, r.Name)
	}

	return nil
}

// getRootLvolName relies on the lvol name to identify if a lvol belongs to the replica,
// then figuring out whether it is the root by checking the parent
func getRootLvolName(replicaName string, bdevLvolMap map[string]*spdktypes.BdevInfo) (rootLvolName string) {
	for lvolName, bdevLvol := range bdevLvolMap {
		if lvolName != replicaName && !IsReplicaSnapshotLvol(replicaName, lvolName) {
			continue
		}
		// Consider that a backing image can be the parent of the replica root
		if bdevLvol.DriverSpecific.Lvol.BaseSnapshot != "" && IsReplicaSnapshotLvol(replicaName, bdevLvol.DriverSpecific.Lvol.BaseSnapshot) {
			continue
		}
		return lvolName
	}

	return ""
}

func constructSnapshotLvolMap(replicaName string, bdevLvolMap map[string]*spdktypes.BdevInfo) (res map[string]*Lvol, err error) {
	rootLvolName := getRootLvolName(replicaName, bdevLvolMap)
	if rootLvolName == "" {
		return nil, fmt.Errorf("cannot find the root of the replica during snapshot lvol map construction")
	}
	res = map[string]*Lvol{}

	queue := []*Lvol{BdevLvolInfoToServiceLvol(bdevLvolMap[rootLvolName])}
	for ; len(queue) > 0; queue = queue[1:] {
		curSvcLvol := queue[0]
		if curSvcLvol == nil || curSvcLvol.Name == replicaName {
			continue
		}
		if !IsReplicaSnapshotLvol(replicaName, curSvcLvol.Name) {
			continue
		}
		res[curSvcLvol.Name] = curSvcLvol

		if bdevLvolMap[curSvcLvol.Name].DriverSpecific.Lvol.Clones == nil {
			continue
		}
		for _, childLvolName := range bdevLvolMap[curSvcLvol.Name].DriverSpecific.Lvol.Clones {
			if bdevLvolMap[childLvolName] == nil {
				return nil, fmt.Errorf("cannot find child lvol %v for lvol %v during the snapshot lvol map construction", childLvolName, curSvcLvol.Name)
			}
			curSvcLvol.Children[childLvolName] = BdevLvolInfoToServiceLvol(bdevLvolMap[childLvolName])
			queue = append(queue, curSvcLvol.Children[childLvolName])
		}
	}

	return res, nil
}

// constructActiveChainFromSnapshotLvolMap retrieves the chain bottom up (from the head to the ancestor snapshot/backing image).
func constructActiveChainFromSnapshotLvolMap(replicaName string, snapshotLvolMap map[string]*Lvol, bdevLvolMap map[string]*spdktypes.BdevInfo) (res []*Lvol, err error) {
	headBdevLvol := bdevLvolMap[replicaName]
	if headBdevLvol == nil {
		return nil, fmt.Errorf("found nil head bdev lvol for replica %s", replicaName)
	}

	var headSvcLvol *Lvol
	headParentSnapshotName := headBdevLvol.DriverSpecific.Lvol.BaseSnapshot
	if IsReplicaSnapshotLvol(replicaName, headParentSnapshotName) {
		headParentSnapSvcLvol := snapshotLvolMap[headParentSnapshotName]
		if headParentSnapSvcLvol == nil {
			fmt.Errorf("cannot find the parent snapshot %s of the head for replica %s", headParentSnapshotName, replicaName)
		}
		headSvcLvol = headParentSnapSvcLvol.Children[replicaName]
	} else { // The parent of the head is nil or a backing image
		headSvcLvol = BdevLvolInfoToServiceLvol(headBdevLvol)
	}
	if headSvcLvol == nil {
		return nil, fmt.Errorf("found nil head svc lvol for replica %s", replicaName)
	}

	newChain := []*Lvol{headSvcLvol}
	// TODO: Considering the clone, this function or `constructSnapshotMap` may need to construct the children map for the head

	// Build the majority of the chain with `snapshotMap` so that it does not need to worry about the snap svc lvol children map maintenance.
	for curSvcLvol := snapshotLvolMap[headSvcLvol.Parent]; curSvcLvol != nil; curSvcLvol = snapshotLvolMap[curSvcLvol.Parent] {
		newChain = append(newChain, curSvcLvol)
	}

	// Check if the root snap/head lvol has a parent. If YES, it means that this replica contains a backing image
	var biSvcLvol *Lvol
	rootLvol := newChain[len(newChain)-1]
	if rootLvol.Parent != "" {
		// Here we won't maintain the complete children map for the backing image Lvol since it may contain root lvols of other replicas
		biBdevLvol := bdevLvolMap[rootLvol.Parent]
		if biBdevLvol == nil {
			return nil, fmt.Errorf("cannot find backing image lvol %v for the current bdev lvol map for replica %s", rootLvol.Parent, replicaName)
		}
		biSvcLvol = BdevLvolInfoToServiceLvol(biBdevLvol)
		biSvcLvol.Children[rootLvol.Name] = rootLvol
	}
	newChain = append(newChain, biSvcLvol)

	// Need to flip r.ActiveSnapshotChain. By convention the oldest one (backing image) should be at index 0
	for head, tail := 0, len(newChain)-1; head < tail; head, tail = head+1, tail-1 {
		newChain[head], newChain[tail] = newChain[tail], newChain[head]
	}

	return newChain, nil
}

func (r *Replica) Create(spdkClient *spdkclient.Client, exposeRequired bool, portCount int32, superiorPortAllocator *util.Bitmap) (ret *spdkrpc.Replica, err error) {
	updateRequired := true

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State == types.InstanceStateRunning {
		updateRequired = false
		return nil, grpcstatus.Errorf(grpccodes.AlreadyExists, "replica %v already exists and running", r.Name)
	}
	if r.State != types.InstanceStatePending && r.State != types.InstanceStateStopped {
		updateRequired = false
		return nil, fmt.Errorf("invalid state %s for replica %s creation", r.State, r.Name)
	}

	defer func() {
		if err != nil {
			r.log.WithError(err).Errorf("Failed to create replica %s", r.Name)
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
			}
			r.ErrorMsg = err.Error()

			ret = ServiceReplicaToProtoReplica(r)
			err = nil
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	if r.ChainLength < 2 {
		return nil, fmt.Errorf("invalid chain length %d for replica creation", r.ChainLength)
	}
	headSvcLvol := r.ActiveChain[r.ChainLength-1]

	// Create bdev lvol if the replica is the new one
	if r.State == types.InstanceStatePending {
		var lvsList []spdktypes.LvstoreInfo
		if r.LvsUUID != "" {
			lvsList, err = spdkClient.BdevLvolGetLvstore("", r.LvsUUID)
		} else if r.LvsName != "" {
			lvsList, err = spdkClient.BdevLvolGetLvstore(r.LvsName, "")
		}
		if err != nil {
			return nil, err
		}
		if len(lvsList) != 1 {
			return nil, fmt.Errorf("found zero or multiple lvstore with name %s and UUID %s during replica %s creation", r.LvsName, r.LvsUUID, r.Name)
		}
		if r.LvsName == "" {
			r.LvsName = lvsList[0].Name
		}
		if r.LvsUUID == "" {
			r.LvsUUID = lvsList[0].UUID
		}
		if r.LvsName != lvsList[0].Name || r.LvsUUID != lvsList[0].UUID {
			return nil, fmt.Errorf("found mismatching between the actual lvstore name %s with UUID %s and the recorded lvstore name %s with UUID %s during replica %s creation", lvsList[0].Name, lvsList[0].UUID, r.LvsName, r.LvsUUID, r.Name)
		}

		r.log.Info("Creating a lvol bdev for the new replica")
		if _, err := spdkClient.BdevLvolCreate("", r.LvsUUID, r.Name, util.BytesToMiB(r.SpecSize), "", true); err != nil {
			return nil, err
		}
		bdevLvolList, err := spdkClient.BdevLvolGet(r.Alias, 0)
		if err != nil {
			return nil, err
		}
		if len(bdevLvolList) < 1 {
			return nil, fmt.Errorf("cannot find lvol %v after creation", r.Alias)
		}
		headSvcLvol.UUID = bdevLvolList[0].UUID
		headSvcLvol.CreationTime = bdevLvolList[0].CreationTime
		headSvcLvol.ActualSize = bdevLvolList[0].DriverSpecific.Lvol.NumAllocatedClusters * defaultClusterSize
		r.State = types.InstanceStateStopped
	}

	podIP, err := commonNet.GetIPForPod()
	if err != nil {
		return nil, err
	}
	r.IP = podIP

	r.PortStart, r.PortEnd, err = superiorPortAllocator.AllocateRange(portCount)
	if err != nil {
		return nil, err
	}
	// Always reserved the 1st port for replica expose and the rest for rebuilding
	r.portAllocator = util.NewBitmap(r.PortStart+1, r.PortEnd)

	if exposeRequired {
		if err := spdkClient.StartExposeBdev(helpertypes.GetNQN(r.Name), headSvcLvol.UUID, podIP, strconv.Itoa(int(r.PortStart))); err != nil {
			return nil, err
		}
		r.IsExposeRequired = true
		r.IsExposed = true
	}
	r.State = types.InstanceStateRunning

	r.log.Info("Created replica")

	return ServiceReplicaToProtoReplica(r), nil
}

func (r *Replica) Delete(spdkClient *spdkclient.Client, cleanupRequired bool, superiorPortAllocator *util.Bitmap) (err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		// Considering that there may be still pending validations, it's better to update the state after the deletion.
		prevState := r.State
		if err != nil {
			r.log.WithError(err).Errorf("Failed to delete replica with cleanupRequired flag %v", cleanupRequired)
			if r.isRestoring {
				// This is not a real error. No need to update the state.
			} else if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				r.ErrorMsg = err.Error()
			}
		} else {
			if !r.isRestoring {
				if cleanupRequired {
					r.State = types.InstanceStateTerminating
				} else {
					r.State = types.InstanceStateStopped
				}
			}
		}

		if r.State != types.InstanceStateError {
			r.ErrorMsg = ""
		}

		if prevState != r.State {
			updateRequired = true
		}

		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	// TODO: Need to stop all in-progress rebuilding first
	// if err := r.rebuildingDstCleanup(spdkClient); err != nil {
	// 	return err
	// }

	if r.isRestoring {
		r.log.Info("Canceling volume restoration before replica deletion")
		r.restore.Stop()
		return fmt.Errorf("waiting for volume restoration to stop")
	}

	if r.IsExposed {
		r.log.Info("Unexposing lvol bdev for replica deletion")
		if err := spdkClient.StopExposeBdev(helpertypes.GetNQN(r.Name)); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return err
		}
		r.IsExposed = false
		r.rebuildingPort = 0
		updateRequired = true
	}
	r.rebuildingLvol = nil
	r.isRebuilding = false

	// The port can be released once the rebuilding and expose are stopped.
	if r.PortStart != 0 {
		if err := superiorPortAllocator.ReleaseRange(r.PortStart, r.PortEnd); err != nil {
			return errors.Wrapf(err, "failed to release port %d to %d during replica deletion with cleanup flag %v", r.PortStart, r.PortEnd, cleanupRequired)
		}
		r.portAllocator = nil
		r.PortStart, r.PortEnd = 0, 0
		updateRequired = true
	}

	if !cleanupRequired {
		return nil
	}

	// Use r.Alias here since we don't know if an errored replicas still contains the head lvol
	if _, err := spdkClient.BdevLvolDelete(r.Alias); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}

	updateRequired = true

	// Retrieve the snapshot tree with BFS. Then do cleanup bottom up
	var queue []*Lvol
	if len(r.ActiveChain) > 1 {
		queue = []*Lvol{r.ActiveChain[1]}
	}
	for idx := 0; idx < len(queue); idx++ {
		for _, child := range queue[idx].Children {
			queue = append(queue, child)
		}
	}
	for idx := len(queue) - 1; idx >= 0; idx-- {
		if _, err := spdkClient.BdevLvolDelete(queue[idx].UUID); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return err
		}
	}

	r.log.Info("Deleted replica")

	return nil
}

func (r *Replica) Get() (pReplica *spdkrpc.Replica) {
	r.RLock()
	defer r.RUnlock()
	return ServiceReplicaToProtoReplica(r)
}

func (r *Replica) SnapshotCreate(spdkClient *spdkclient.Client, snapshotName string) (pReplica *spdkrpc.Replica, err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateStopped && r.State != types.InstanceStateRunning {
		return nil, fmt.Errorf("invalid state %v for replica %s snapshot creation", r.State, r.Name)
	}

	snapLvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
	if _, exists := r.SnapshotLvolMap[snapLvolName]; exists {
		return nil, fmt.Errorf("snapshot %s(%s) already exists in replica %s", snapshotName, snapLvolName, r.Name)
	}

	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				updateRequired = true
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	if r.ChainLength < 2 {
		return nil, fmt.Errorf("invalid chain length %d for replica snapshot creation", r.ChainLength)
	}
	headSvcLvol := r.ActiveChain[r.ChainLength-1]

	snapUUID, err := spdkClient.BdevLvolSnapshot(headSvcLvol.UUID, snapLvolName, nil)
	if err != nil {
		return nil, err
	}

	bdevLvolList, err := spdkClient.BdevLvolGet(snapUUID, 0)
	if err != nil {
		return nil, err
	}
	if len(bdevLvolList) != 1 {
		return nil, fmt.Errorf("zero or multiple snap lvols with UUID %s found after lvol snapshot", snapUUID)
	}

	snapSvcLvol := BdevLvolInfoToServiceLvol(&bdevLvolList[0])
	snapSvcLvol.Children[headSvcLvol.Name] = headSvcLvol

	// Already contain a valid snapshot lvol or backing image lvol before this snapshot creation
	if r.ActiveChain[r.ChainLength-2] != nil {
		prevSvcLvol := r.ActiveChain[r.ChainLength-2]
		delete(prevSvcLvol.Children, headSvcLvol.Name)
		prevSvcLvol.Children[snapSvcLvol.Name] = snapSvcLvol
	}
	r.ActiveChain[r.ChainLength-1] = snapSvcLvol
	r.ActiveChain = append(r.ActiveChain, headSvcLvol)
	r.ChainLength++
	r.SnapshotLvolMap[snapLvolName] = snapSvcLvol
	headSvcLvol.Parent = snapSvcLvol.Name
	updateRequired = true

	r.log.Infof("Replica created snapshot %s(%s)", snapshotName, snapSvcLvol.Alias)

	return ServiceReplicaToProtoReplica(r), err
}

func (r *Replica) SnapshotDelete(spdkClient *spdkclient.Client, snapshotName string) (pReplica *spdkrpc.Replica, err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateStopped && r.State != types.InstanceStateRunning {
		return nil, fmt.Errorf("invalid state %v for replica %s snapshot deletion", r.State, r.Name)
	}

	snapLvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
	snapSvcLvol := r.SnapshotLvolMap[snapLvolName]
	if snapSvcLvol == nil {
		return ServiceReplicaToProtoReplica(r), nil
	}
	if len(snapSvcLvol.Children) > 1 {
		return nil, fmt.Errorf("cannot delete snapshot %s(%s) since it has %d children", snapshotName, snapLvolName, len(snapSvcLvol.Children))
	}

	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				updateRequired = true
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	if r.ChainLength < 2 {
		return nil, fmt.Errorf("invalid chain length %d for replica snapshot delete", r.ChainLength)
	}

	if _, err := spdkClient.BdevLvolDelete(snapSvcLvol.UUID); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return nil, err
	}
	r.removeLvolFromSnapshotLvolMapWithoutLock(snapLvolName)
	r.removeLvolFromActiveChainWithoutLock(snapLvolName)
	for _, childSvcLvol := range snapSvcLvol.Children {
		bdevLvol, err := spdkClient.BdevLvolGet(childSvcLvol.UUID, 0)
		if err != nil {
			return nil, err
		}
		if len(bdevLvol) != 1 {
			return nil, fmt.Errorf("failed to get the bdev of the only child lvol %s after snapshot %s delete", childSvcLvol.Name, snapshotName)
		}
		childSvcLvol.ActualSize = bdevLvol[0].DriverSpecific.Lvol.NumAllocatedClusters * defaultClusterSize
	}

	updateRequired = true

	r.log.Infof("Replica deleted snapshot %s(%s)", snapshotName, snapSvcLvol.Alias)

	return ServiceReplicaToProtoReplica(r), nil
}

func (r *Replica) removeLvolFromSnapshotLvolMapWithoutLock(snapsLvolName string) {
	var deletingSvcLvol, parentSvcLvol, childSvcLvol *Lvol

	deletingSvcLvol = r.SnapshotLvolMap[snapsLvolName]
	if IsReplicaSnapshotLvol(r.Name, deletingSvcLvol.Parent) {
		parentSvcLvol = r.SnapshotLvolMap[deletingSvcLvol.Parent]
	} else {
		parentSvcLvol = r.ActiveChain[0]
	}
	if parentSvcLvol != nil {
		delete(parentSvcLvol.Children, deletingSvcLvol.Name)
	}
	for _, childSvcLvol = range deletingSvcLvol.Children {
		if parentSvcLvol != nil {
			parentSvcLvol.Children[childSvcLvol.Name] = childSvcLvol
			childSvcLvol.Parent = parentSvcLvol.Name
		} else {
			childSvcLvol.Parent = ""
		}
	}

	delete(r.SnapshotLvolMap, snapsLvolName)
}

func (r *Replica) removeLvolFromActiveChainWithoutLock(snapLvolName string) int {
	pos := -1
	for idx, lvol := range r.ActiveChain {
		// Cannot remove the backing image from the chain
		if idx == 0 {
			continue
		}
		if lvol.Name == snapLvolName {
			pos = idx
			break
		}
	}

	// Cannot remove backing image lvol or head lvol
	prevChain := r.ActiveChain
	if pos >= 1 && pos < r.ChainLength-1 {
		r.ActiveChain = append([]*Lvol{}, prevChain[:pos]...)
		r.ActiveChain = append(r.ActiveChain, prevChain[pos+1:]...)
	}
	r.ChainLength = len(r.ActiveChain)

	return pos
}

func (r *Replica) SnapshotRevert(spdkClient *spdkclient.Client, snapshotName string) (pReplica *spdkrpc.Replica, err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateStopped && r.State != types.InstanceStateRunning {
		return nil, fmt.Errorf("invalid state %v for replica %s snapshot revert", r.State, r.Name)
	}

	snapLvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
	snapSvcLvol := r.SnapshotLvolMap[snapLvolName]
	if snapSvcLvol == nil {
		return nil, fmt.Errorf("cannot revert to a non-existing snapshot %s(%s)", snapshotName, snapLvolName)
	}

	defer func() {
		if err != nil && r.State != types.InstanceStateError {
			r.State = types.InstanceStateError
			updateRequired = true
		}
	}()

	if r.ChainLength < 2 {
		return nil, fmt.Errorf("invalid chain length %d for replica snapshot revert", r.ChainLength)
	}

	if _, err := spdkClient.BdevLvolDelete(r.Alias); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return nil, err
	}
	// The parent of the old head lvol is a valid snapshot lvol or backing image lvol
	if r.ActiveChain[r.ChainLength-2] != nil {
		delete(r.ActiveChain[r.ChainLength-2].Children, r.Name)
	}
	r.ChainLength--
	r.ActiveChain = r.ActiveChain[:r.ChainLength]

	// TODO: If the below steps fail, there will be no head lvol for the replica. Need to guarantee that the replica can be cleaned up correctly in this case

	headLvolUUID, err := spdkClient.BdevLvolClone(snapSvcLvol.UUID, r.Name)
	if err != nil {
		return nil, err
	}

	bdevLvolMap, err := GetBdevLvolMap(spdkClient)
	if err != nil {
		return nil, err
	}

	newSnapshotLvolMap, err := constructSnapshotLvolMap(r.Name, bdevLvolMap)
	if err != nil {
		return nil, err
	}
	newChain, err := constructActiveChainFromSnapshotLvolMap(r.Name, newSnapshotLvolMap, bdevLvolMap)
	if err != nil {
		return nil, err
	}

	r.ActiveChain = newChain
	r.ChainLength = len(r.ActiveChain)
	r.SnapshotLvolMap = newSnapshotLvolMap

	if r.IsExposed {
		if err := spdkClient.StopExposeBdev(helpertypes.GetNQN(r.Name)); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return nil, err
		}
		if err := spdkClient.StartExposeBdev(helpertypes.GetNQN(r.Name), headLvolUUID, r.IP, strconv.Itoa(int(r.PortStart))); err != nil {
			return nil, err
		}
	}

	updateRequired = true

	r.log.Infof("Replica reverted snapshot %s(%s)", snapshotName, snapSvcLvol.Alias)

	return ServiceReplicaToProtoReplica(r), nil
}

func (r *Replica) RebuildingSrcStart(spdkClient *spdkclient.Client, localReplicaLvsNameMap map[string]string, dstReplicaName, dstRebuildingLvolAddress string) (err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %v for replica %s rebuilding src start", r.State, r.Name)
	}
	if r.rebuildingLvol != nil || r.rebuildingPort != 0 {
		return fmt.Errorf("replica %s is being rebuilding hence it cannot be the source of rebuilding replica %s", r.Name, dstReplicaName)
	}

	dstRebuildingLvolIP, _, err := net.SplitHostPort(dstRebuildingLvolAddress)
	if err != nil {
		return err
	}
	// TODO: After launching online rebuilding, the destination lvol name would be GetReplicaRebuildingLvolName(dstReplicaName)
	dstRebuildingLvolName := dstReplicaName

	if dstRebuildingLvolIP == r.IP {
		dstReplicaLvsName := localReplicaLvsNameMap[dstReplicaName]
		if dstReplicaLvsName == "" {
			return fmt.Errorf("cannot find dst replica %s from the local replica map for replica %s rebuilding src start", dstReplicaName, r.Name)
		}
		r.rebuildingDstBdevName = spdktypes.GetLvolAlias(dstReplicaLvsName, dstRebuildingLvolName)
		r.rebuildingDstBdevType = spdktypes.BdevTypeLvol
	} else {
		r.rebuildingDstBdevType = spdktypes.BdevTypeNvme
		if err = r.RebuildingSrcAttach(spdkClient, dstReplicaName, dstRebuildingLvolAddress); err != nil {
			r.rebuildingDstBdevType = ""
			return err
		}
	}
	r.rebuildingDstReplicaName = dstReplicaName
	updateRequired = true

	return nil
}

func (r *Replica) RebuildingSrcFinish(spdkClient *spdkclient.Client, dstReplicaName string) (err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.rebuildingDstReplicaName == "" && r.rebuildingDstBdevName == "" && r.rebuildingDstBdevType == "" {
		return nil
	}

	if r.rebuildingDstReplicaName != "" && r.rebuildingDstReplicaName != dstReplicaName {
		return fmt.Errorf("found mismatching between the required dst replica name %s and the recorded dst replica name %s for replica %s rebuilding src finish", dstReplicaName, r.rebuildingDstReplicaName, r.Name)
	}

	// TODO: After launching online rebuilding, the destination lvol name would be GetReplicaRebuildingLvolName(dstReplicaName)
	dstRebuildingLvolName := dstReplicaName
	switch r.rebuildingDstBdevType {
	case spdktypes.BdevTypeLvol:
		lvolName := spdktypes.GetLvolNameFromAlias(r.rebuildingDstBdevName)
		if dstRebuildingLvolName != lvolName {
			r.log.Errorf("Found mismatching between the required dst bdev lvol name %d and the expected dst lvol name %d for replica %s rebuilding src finish, will do nothing but just clean up rebuilding dst info", dstRebuildingLvolName, lvolName, r.Name)
		}
	case spdktypes.BdevTypeNvme:
		if err = r.RebuildingSrcDetach(spdkClient, dstReplicaName); err != nil {
			r.log.WithError(err).Errorf("Failed to detach rebuilding dst dev %s for dst replica %s, will do nothing but just clean up rebuilding dst info", r.rebuildingDstBdevName, r.rebuildingDstReplicaName)
		}
	default:
		r.log.Errorf("Found unknown rebuilding dst bdev type %s with name %s for replica %s rebuilding src finish, will do nothing but just clean up rebuilding dst info", r.rebuildingDstBdevType, r.rebuildingDstBdevName, r.Name)
	}

	r.rebuildingDstReplicaName = ""
	r.rebuildingDstBdevName = ""
	r.rebuildingDstBdevType = ""
	updateRequired = true

	return nil
}

func (r *Replica) RebuildingSrcAttach(spdkClient *spdkclient.Client, dstReplicaName, dstRebuildingLvolAddress string) (err error) {
	if r.rebuildingDstBdevType != spdktypes.BdevTypeNvme {
		return nil
	}

	// TODO: After launching online rebuilding, the destination lvol name would be GetReplicaRebuildingLvolName(dstReplicaName)
	dstRebuildingLvolName := dstReplicaName
	if r.rebuildingDstBdevName != "" {
		controllerName := helperutil.GetNvmeControllerNameFromNamespaceName(r.rebuildingDstBdevName)
		if dstRebuildingLvolName != controllerName {
			return fmt.Errorf("found mismatching between the required dst bdev nvme controller name %s and the expected dst controller name %s for replica %s rebuilding src attach", dstRebuildingLvolName, controllerName, r.Name)
		}
		return nil
	}

	dstRebuildingLvolIP, dstRebuildingLvolPort, err := net.SplitHostPort(dstRebuildingLvolAddress)
	if err != nil {
		return err
	}
	if dstRebuildingLvolIP == r.IP {
		return fmt.Errorf("cannot attach the rebuilding dst when its IP is the same as the current src replica IP")
	}

	nvmeBdevNameList, err := spdkClient.BdevNvmeAttachController(dstRebuildingLvolName, helpertypes.GetNQN(dstRebuildingLvolName),
		dstRebuildingLvolIP, dstRebuildingLvolPort, spdktypes.NvmeTransportTypeTCP, spdktypes.NvmeAddressFamilyIPv4,
		helpertypes.DefaultCtrlrLossTimeoutSec, helpertypes.DefaultReconnectDelaySec, helpertypes.DefaultFastIOFailTimeoutSec)
	if err != nil {
		return err
	}
	if len(nvmeBdevNameList) != 1 {
		return fmt.Errorf("got zero or multiple results when attaching rebuilding dst lvol %s with address %s as a NVMe bdev: %+v", dstRebuildingLvolName, dstRebuildingLvolAddress, nvmeBdevNameList)
	}
	r.rebuildingDstBdevName = nvmeBdevNameList[0]

	return nil
}

func (r *Replica) RebuildingSrcDetach(spdkClient *spdkclient.Client, dstReplicaName string) (err error) {
	if r.rebuildingDstBdevType != spdktypes.BdevTypeNvme {
		return nil
	}
	if r.rebuildingDstBdevName == "" {
		return nil
	}

	// TODO: After launching online rebuilding, the destination lvol name would be GetReplicaRebuildingLvolName(dstReplicaName)
	dstRebuildingLvolName := dstReplicaName
	controllerName := helperutil.GetNvmeControllerNameFromNamespaceName(r.rebuildingDstBdevName)
	if dstRebuildingLvolName != controllerName {
		return fmt.Errorf("found mismatching between the required dst bdev nvme controller name %s and the expected dst controller name %s for replica %s rebuilding src detach", dstRebuildingLvolName, controllerName, r.Name)
	}

	if _, err := spdkClient.BdevNvmeDetachController(controllerName); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}
	r.rebuildingDstBdevName = ""

	return err
}

func (r *Replica) SnapshotShallowCopy(spdkClient *spdkclient.Client, snapshotName string) (err error) {
	r.RLock()
	snapLvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
	srcSnapLvol := r.SnapshotLvolMap[snapLvolName]
	dstBdevName := r.rebuildingDstBdevName
	r.RUnlock()

	if dstBdevName == "" {
		return fmt.Errorf("no destination bdev for replica %s shallow copy", r.Name)
	}
	if srcSnapLvol == nil {
		return fmt.Errorf("cannot find snapshot %s for replica %s shallow copy", snapshotName, r.Name)
	}

	_, err = spdkClient.BdevLvolShallowCopy(srcSnapLvol.UUID, dstBdevName)
	return err
}

func (r *Replica) RebuildingDstStart(spdkClient *spdkclient.Client, exposeRequired bool) (address string, err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateRunning {
		return "", fmt.Errorf("invalid state %v for replica %s rebuilding start", r.State, r.Name)
	}
	if r.isRebuilding || r.rebuildingLvol != nil {
		return "", fmt.Errorf("replica %s rebuilding is in process", r.Name)
	}

	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				updateRequired = true
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	// TODO: When online rebuilding related APIs are ready, we need to create a separate and temporary lvol for snapshot lvol rebuilding
	// lvolName := GetReplicaRebuildingLvolName(r.Name)
	// if _, err := spdkClient.BdevLvolCreate("", r.LvsUUID, lvolName, util.BytesToMiB(r.SpecSize), "", true); err != nil {
	// 	return "", err
	// }
	// bdevLvolList, err := spdkClient.BdevLvolGet(r.Alias, 0)
	// if err != nil {
	// 	return "", err
	// }
	// if len(bdevLvolList) < 1 {
	// 	return "", fmt.Errorf("cannot find lvol %v after rebuilding lvol creation", spdktypes.GetLvolAlias(r.LvsName, lvolName))
	// }
	// r.rebuildingLvol = &Lvol{
	// 	Name:     lvolName,
	// 	UUID:     bdevLvolList[0].UUID,
	// 	Alias:    bdevLvolList[0].Aliases[0],
	// 	SpecSize: r.SpecSize,
	// 	Children: map[string]*Lvol{},
	// }
	// if exposeRequired {
	// 	r.rebuildingPort, _, err = r.portAllocator.AllocateRange(1)
	// 	if err != nil {
	// 		return "", err
	// 	}
	// 	if err := spdkClient.StartExposeBdev(helpertypes.GetNQN(lvolName), r.rebuildingLvol.UUID, r.IP, strconv.Itoa(int(r.rebuildingPort))); err != nil {
	// 		return "", err
	// 	}
	// }

	r.isRebuilding = true
	// TODO: Currently, the online rebuilding related APIs are not ready, we use the exposed head lvol for snapshot lvol rebuilding
	// TODO: Will use r.portAllocator rather than the reserved first port for rebuilding in the future version
	r.rebuildingLvol = r.ActiveChain[r.ChainLength-1]
	if exposeRequired {
		if !r.IsExposed {
			if err := spdkClient.StartExposeBdev(helpertypes.GetNQN(r.rebuildingLvol.Name), r.rebuildingLvol.UUID, r.IP, strconv.Itoa(int(r.PortStart))); err != nil {
				return "", err
			}
			r.IsExposeRequired = true
			r.IsExposed = true
		}
		r.rebuildingPort = r.PortStart
	}

	return net.JoinHostPort(r.IP, strconv.Itoa(int(r.rebuildingPort))), nil
}

func (r *Replica) RebuildingDstFinish(spdkClient *spdkclient.Client, unexposeRequired bool) (err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %v for replica %s rebuilding finish", r.State, r.Name)
	}
	if !r.isRebuilding {
		return fmt.Errorf("replica %s is not in rebuilding", r.Name)
	}
	if r.rebuildingLvol == nil {
		return fmt.Errorf("cannot find rebuilding lvol for replica %s rebuilding finish", r.Name)
	}

	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				updateRequired = true
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
		r.isRebuilding = false
	}()

	// TODO: For the online rebuilding, Remove the temporary rebuilding lvol and release ports for r.portAllocator
	// if err := r.rebuildingDstCleanup(spdkClient); err != nil {
	// 	return err
	// }

	// For the current offline rebuilding, the temporary rebuilding lvol is actually the head lvol hence there is no need to remove it
	if unexposeRequired {
		if err := spdkClient.StopExposeBdev(helpertypes.GetNQN(r.rebuildingLvol.Name)); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return err
		}
		r.IsExposeRequired = false
		r.IsExposed = false
	}
	r.rebuildingPort = 0
	r.rebuildingLvol = nil

	// TODO: For online rebuilding, connect the snapshot tree/chain with the head lvol then mark the head lvol as mode RW

	bdevLvolList, err := spdkClient.BdevLvolGet("", 0)
	if err != nil {
		return err
	}
	bdevLvolMap := map[string]*spdktypes.BdevInfo{}
	for idx := range bdevLvolList {
		bdevLvol := &bdevLvolList[idx]
		lvolName := spdktypes.GetLvolNameFromAlias(bdevLvol.Aliases[0])
		bdevLvolMap[lvolName] = bdevLvol
	}

	return r.construct(bdevLvolMap)
}

// func (r *Replica) rebuildingDstCleanup(spdkClient *spdkclient.Client) error {
// 	if r.rebuildingPort != 0 {
// 		if err := spdkClient.StopExposeBdev(helpertypes.GetNQN(r.rebuildingLvol.Name)); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
// 			return err
// 		}
// 		if err := r.portAllocator.ReleaseRange(r.rebuildingPort, r.rebuildingPort); err != nil {
// 			return err
// 		}
// 		r.rebuildingPort = 0
// 	}
// 	if r.rebuildingLvol != nil {
// 		if _, err := spdkClient.BdevLvolDelete(r.rebuildingLvol.UUID); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
// 			return err
// 		}
// 		r.rebuildingLvol = nil
// 	}
// 	return nil
// }

func (r *Replica) RebuildingDstSnapshotCreate(spdkClient *spdkclient.Client, snapshotName string) (err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %v for replica %s rebuilding snapshot %s creation", r.State, r.Name, snapshotName)
	}
	if !r.isRebuilding {
		return fmt.Errorf("replica %s is not in rebuilding", r.Name)
	}
	if r.rebuildingLvol == nil || (r.IsExposed && r.rebuildingPort == 0) {
		return fmt.Errorf("rebuilding lvol is not existed, or exposed without rebuilding port for replica %s rebuilding snapshot %s creation", r.Name, snapshotName)
	}

	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				updateRequired = true
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	snapLvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
	snapUUID, err := spdkClient.BdevLvolSnapshot(r.rebuildingLvol.UUID, snapLvolName, nil)
	if err != nil {
		return err
	}

	bdevLvolList, err := spdkClient.BdevLvolGet(snapUUID, 0)
	if err != nil {
		return err
	}
	if len(bdevLvolList) != 1 {
		return fmt.Errorf("zero or multiple snap lvols with UUID %s found after rebuilding snapshot %s creation", snapUUID, snapshotName)
	}
	snapSvcLvol := BdevLvolInfoToServiceLvol(&bdevLvolList[0])

	// Do not update r.ActiveChain since we don't know which branch of the snapshot tree is the active one.
	r.SnapshotLvolMap[snapLvolName] = snapSvcLvol
	snapSvcLvol.Children[r.rebuildingLvol.Name] = r.rebuildingLvol
	r.rebuildingLvol.Parent = snapSvcLvol.Name
	updateRequired = true

	return nil
}

func (r *Replica) RebuildingDstSnapshotRevert(spdkClient *spdkclient.Client, snapshotName string) (err error) {
	updateRequired := false

	r.Lock()
	defer func() {
		r.Unlock()

		if updateRequired {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %v for replica %s rebuilding snapshot %s creation", r.State, r.Name, snapshotName)
	}
	if !r.isRebuilding {
		return fmt.Errorf("replica %s is not in rebuilding", r.Name)
	}
	if r.rebuildingLvol == nil || (r.IsExposed && r.rebuildingPort == 0) {
		return fmt.Errorf("rebuilding lvol is not existed, or exposed without rebuilding port for replica %s rebuilding snapshot %s creation", r.Name, snapshotName)
	}

	defer func() {
		if err != nil {
			if r.State != types.InstanceStateError {
				r.State = types.InstanceStateError
				updateRequired = true
			}
			r.ErrorMsg = err.Error()
		} else {
			if r.State != types.InstanceStateError {
				r.ErrorMsg = ""
			}
		}
	}()

	snapLvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
	snapLvolAlias := spdktypes.GetLvolAlias(r.LvsName, snapLvolName)

	if r.IsExposed {
		if err := spdkClient.StopExposeBdev(helpertypes.GetNQN(r.rebuildingLvol.Name)); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return err
		}
	}
	if _, err := spdkClient.BdevLvolDelete(r.rebuildingLvol.UUID); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}

	rebuildingLvolUUID, err := spdkClient.BdevLvolClone(snapLvolAlias, r.rebuildingLvol.Name)
	if err != nil {
		return err
	}

	bdevLvolMap, err := GetBdevLvolMap(spdkClient)
	if err != nil {
		return err
	}
	newSnapshotLvolMap, err := constructSnapshotLvolMap(r.Name, bdevLvolMap)
	if err != nil {
		return err
	}
	r.SnapshotLvolMap = newSnapshotLvolMap
	r.rebuildingLvol = r.SnapshotLvolMap[snapLvolName].Children[r.rebuildingLvol.Name]
	if r.rebuildingLvol.UUID != rebuildingLvolUUID {
		return fmt.Errorf("new rebuilding lvol %v UUID %v does not match the list result %v after snapshot %v revert", r.rebuildingLvol.Name, rebuildingLvolUUID, r.rebuildingLvol.UUID, snapshotName)
	}

	if r.IsExposed {
		if err := spdkClient.StartExposeBdev(helpertypes.GetNQN(r.rebuildingLvol.Name), r.rebuildingLvol.UUID, r.IP, strconv.Itoa(int(r.PortStart))); err != nil {
			return err
		}
	}

	updateRequired = true

	r.log.Infof("Rebuilding destination replica reverted snapshot %s(%s)", snapshotName, snapLvolAlias)

	return nil
}

func (r *Replica) BackupRestore(spdkClient *spdkclient.Client, backupUrl, snapshotName string, credential map[string]string, concurrentLimit int32) (err error) {
	r.Lock()
	defer r.Unlock()

	defer func() {
		if err == nil {
			r.isRestoring = true
		}
	}()

	if r.isRestoring {
		return fmt.Errorf("cannot initiate backup restore as there is one already in progress")
	}

	backupType, err := butil.CheckBackupType(backupUrl)
	if err != nil {
		err = errors.Wrapf(err, "failed to check the type for restoring backup %v", backupUrl)
		return grpcstatus.Errorf(grpccodes.InvalidArgument, err.Error())
	}

	err = butil.SetupCredential(backupType, credential)
	if err != nil {
		err = errors.Wrapf(err, "failed to setup credential for restoring backup %v", backupUrl)
		return grpcstatus.Errorf(grpccodes.Internal, err.Error())
	}

	backupName, _, _, err := backupstore.DecodeBackupURL(util.UnescapeURL(backupUrl))
	if err != nil {
		err = errors.Wrapf(err, "failed to decode backup url %v", backupUrl)
		return grpcstatus.Errorf(grpccodes.InvalidArgument, err.Error())
	}

	if r.restore == nil {
		return grpcstatus.Errorf(grpccodes.NotFound, "restoration for backup %v is not initialized", backupUrl)
	}

	restore := r.restore.DeepCopy()
	if restore.State == btypes.ProgressStateError {
		return fmt.Errorf("cannot start restoring backup %v of the previous failed restoration", backupUrl)
	}

	if restore.LastRestored == backupName {
		return grpcstatus.Errorf(grpccodes.AlreadyExists, "already restored backup %v", backupName)
	}

	// Initialize `r.restore`
	// First restore request. It must be a normal full restore.
	if restore.LastRestored == "" && (restore.State == btypes.ProgressStateUndefined || restore.State == btypes.ProgressStateCanceled) {
		r.log.Infof("Starting a new restore for backup %v with restore state %v", backupUrl, restore.State)
		lvolName := GetReplicaSnapshotLvolName(r.Name, snapshotName)
		r.restore, err = NewRestore(spdkClient, lvolName, snapshotName, backupUrl, backupName, r)
		if err != nil {
			err = errors.Wrapf(err, "failed to start new restore")
			return grpcstatus.Errorf(grpccodes.Internal, err.Error())
		}
	} else {
		var lvolName string
		var snapshotNameToBeRestored string

		validLastRestoredBackup := r.canDoIncrementalRestore(restore, backupUrl, backupName)
		if validLastRestoredBackup {
			lvolName = GetReplicaSnapshotLvolName(r.Name, restore.LastRestored)
			snapshotNameToBeRestored = restore.LastRestored
		} else {
			lvolName = GetReplicaSnapshotLvolName(r.Name, snapshotName)
			snapshotNameToBeRestored = snapshotName
		}
		r.restore.StartNewRestore(backupUrl, backupName, lvolName, snapshotNameToBeRestored, validLastRestoredBackup)
	}

	// Initiate restore
	newRestore := r.restore.DeepCopy()
	defer func() {
		if err != nil {
			// TODO: Support snapshot revert for incremental restore
		}
	}()

	if newRestore.LastRestored == "" {
		r.log.Infof("Starting a new full restore for backup %v", backupUrl)
		if err := r.backupRestore(backupUrl, newRestore.LvolName, concurrentLimit); err != nil {
			return errors.Wrapf(err, "failed to start full backup restore")
		}
		r.log.Infof("Successfully initiated full restore for %v to %v", backupUrl, newRestore.LvolName)
	} else {
		return fmt.Errorf("incremental restore is not supported yet")
	}

	go r.completeBackupRestore(spdkClient)

	return nil

}

func (r *Replica) backupRestore(backupURL, snapshotLvolName string, concurrentLimit int32) error {
	backupURL = butil.UnescapeURL(backupURL)

	logrus.WithFields(logrus.Fields{
		"backupURL":        backupURL,
		"snapshotLvolName": snapshotLvolName,
		"concurrentLimit":  concurrentLimit,
	}).Info("Start restoring backup")

	return backupstore.RestoreDeltaBlockBackup(r.ctx, &backupstore.DeltaRestoreConfig{
		BackupURL:       backupURL,
		DeltaOps:        r.restore,
		Filename:        snapshotLvolName,
		ConcurrentLimit: int32(concurrentLimit),
	})
}

func (r *Replica) canDoIncrementalRestore(restore *Restore, backupURL, requestedBackupName string) bool {
	if restore.LastRestored == "" {
		logrus.Warnf("There is a restore record in the server but last restored backup is empty with restore state is %v, will do full restore instead", restore.State)
		return false
	}
	if _, err := backupstore.InspectBackup(strings.Replace(backupURL, requestedBackupName, restore.LastRestored, 1)); err != nil {
		logrus.WithError(err).Warnf("The last restored backup %v becomes invalid for incremental restore, will do full restore instead", restore.LastRestored)
		return false
	}
	return true
}

func (r *Replica) completeBackupRestore(spdkClient *spdkclient.Client) (err error) {
	defer func() {
		if extraErr := r.finishRestore(err); extraErr != nil {
			r.log.WithError(extraErr).Error("Failed to finish backup restore")
		}
	}()

	if err := r.waitForRestoreComplete(); err != nil {
		return errors.Wrapf(err, "failed to wait for restore complete")
	}

	r.RLock()
	restore := r.restore.DeepCopy()
	r.RUnlock()

	// TODO: Support postIncrementalRestoreOperations

	return r.postFullRestoreOperations(spdkClient, restore)
}

func (r *Replica) waitForRestoreComplete() error {
	periodicChecker := time.NewTicker(time.Duration(restorePeriodicRefreshInterval.Seconds()) * time.Second)
	defer periodicChecker.Stop()

	for range periodicChecker.C {
		r.restore.RLock()
		restoreProgress := r.restore.Progress
		restoreError := r.restore.Error
		restoreState := r.restore.State
		r.restore.RUnlock()

		if restoreProgress == 100 {
			r.log.Info("Backup restoration completed successfully")
			return nil
		}
		if restoreState == btypes.ProgressStateCanceled {
			r.log.Info("Backup restoration is cancelled")
			return nil
		}
		if restoreError != "" {
			err := fmt.Errorf("%v", restoreError)
			r.log.WithError(err).Errorf("Found backup restoration error")
			return err
		}
	}
	return nil
}

func (r *Replica) postFullRestoreOperations(spdkClient *spdkclient.Client, restore *Restore) error {
	if r.restore.State == btypes.ProgressStateCanceled {
		r.log.Info("Doing nothing for canceled backup restoration")
		return nil
	}

	r.log.Infof("Taking snapshot %v of the restored volume", restore.SnapshotName)

	_, err := r.SnapshotCreate(spdkClient, restore.SnapshotName)
	if err != nil {
		r.log.WithError(err).Error("Failed to take snapshot of the restored volume")
		return errors.Wrapf(err, "failed to take snapshot of the restored volume")
	}

	r.log.Infof("Done running full restore %v to lvol %v (snapshot %v)", restore.BackupURL, restore.LvolName, restore.SnapshotName)
	return nil
}

func (r *Replica) finishRestore(restoreErr error) error {
	r.Lock()
	defer r.Unlock()

	defer func() {
		if r.restore == nil {
			return
		}
		if restoreErr != nil {
			r.restore.UpdateRestoreStatus(r.restore.LvolName, 0, restoreErr)
			return
		}
		r.restore.FinishRestore()
	}()

	if !r.isRestoring {
		err := fmt.Errorf("BUG: volume is not being restored")
		if restoreErr != nil {
			restoreErr = util.CombineErrors(err, restoreErr)
		} else {
			restoreErr = err
		}
		return err
	}

	r.log.Infof("Unflagging isRestoring")
	r.isRestoring = false

	return nil
}

func (r *Replica) SetErrorState() {
	needUpdate := false

	r.Lock()
	defer func() {
		r.Unlock()

		if needUpdate {
			r.UpdateCh <- nil
		}
	}()

	if r.State != types.InstanceStateStopped && r.State != types.InstanceStateError {
		r.State = types.InstanceStateError
		needUpdate = true
	}
}
