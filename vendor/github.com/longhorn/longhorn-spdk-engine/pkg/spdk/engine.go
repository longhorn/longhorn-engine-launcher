package spdk

import (
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

	commonNet "github.com/longhorn/go-common-libs/net"
	commonTypes "github.com/longhorn/go-common-libs/types"
	"github.com/longhorn/go-spdk-helper/pkg/jsonrpc"
	"github.com/longhorn/go-spdk-helper/pkg/nvme"
	spdkclient "github.com/longhorn/go-spdk-helper/pkg/spdk/client"
	spdktypes "github.com/longhorn/go-spdk-helper/pkg/spdk/types"
	helpertypes "github.com/longhorn/go-spdk-helper/pkg/types"
	helperutil "github.com/longhorn/go-spdk-helper/pkg/util"

	"github.com/longhorn/longhorn-spdk-engine/pkg/api"
	"github.com/longhorn/longhorn-spdk-engine/pkg/client"
	"github.com/longhorn/longhorn-spdk-engine/pkg/types"
	"github.com/longhorn/longhorn-spdk-engine/pkg/util"
	"github.com/longhorn/longhorn-spdk-engine/proto/spdkrpc"
)

type Engine struct {
	sync.RWMutex

	Name               string
	VolumeName         string
	SpecSize           uint64
	ActualSize         uint64
	ReplicaAddressMap  map[string]string
	ReplicaBdevNameMap map[string]string
	ReplicaModeMap     map[string]types.Mode
	IP                 string
	Port               int32
	Frontend           string
	Endpoint           string

	dmDeviceBusy bool

	State    types.InstanceState
	ErrorMsg string

	Head        *api.Lvol
	SnapshotMap map[string]*api.Lvol

	IsRestoring bool

	// UpdateCh should not be protected by the engine lock
	UpdateCh chan interface{}

	log logrus.FieldLogger
}

func NewEngine(engineName, volumeName, frontend string, specSize uint64, engineUpdateCh chan interface{}) *Engine {
	log := logrus.StandardLogger().WithFields(logrus.Fields{
		"engineName": engineName,
		"volumeName": volumeName,
		"frontend":   frontend,
	})

	roundedSpecSize := util.RoundUp(specSize, helpertypes.MiB)
	if roundedSpecSize != specSize {
		log.Infof("Rounded up spec size from %v to %v since the spec size should be multiple of MiB", specSize, roundedSpecSize)
	}
	log.WithField("specSize", roundedSpecSize)

	return &Engine{
		Name:               engineName,
		VolumeName:         volumeName,
		Frontend:           frontend,
		SpecSize:           specSize,
		ReplicaAddressMap:  map[string]string{},
		ReplicaBdevNameMap: map[string]string{},
		ReplicaModeMap:     map[string]types.Mode{},

		State: types.InstanceStatePending,

		SnapshotMap: map[string]*api.Lvol{},

		UpdateCh: engineUpdateCh,

		log: log,
	}
}

func (e *Engine) Create(spdkClient *spdkclient.Client, replicaAddressMap, localReplicaLvsNameMap map[string]string, portCount int32, superiorPortAllocator *util.Bitmap) (ret *spdkrpc.Engine, err error) {
	requireUpdate := true

	e.Lock()
	defer func() {
		e.Unlock()

		if requireUpdate {
			e.UpdateCh <- nil
		}
	}()

	if e.State != types.InstanceStatePending {
		requireUpdate = false
		return nil, fmt.Errorf("invalid state %s for engine %s creation", e.State, e.Name)
	}

	defer func() {
		if err != nil {
			e.log.WithError(err).Errorf("Failed to create engine %s", e.Name)
			if e.State != types.InstanceStateError {
				e.State = types.InstanceStateError
			}
			e.ErrorMsg = err.Error()

			ret = e.getWithoutLock()
			err = nil
		} else {
			if e.State != types.InstanceStateError {
				e.ErrorMsg = ""
			}
		}
	}()

	podIP, err := commonNet.GetIPForPod()
	if err != nil {
		return nil, err
	}
	e.IP = podIP
	e.log = e.log.WithField("ip", podIP)

	replicaBdevList := []string{}
	for replicaName, replicaAddr := range replicaAddressMap {
		bdevName, err := e.getBdevNameForReplica(spdkClient, localReplicaLvsNameMap, replicaName, replicaAddr, podIP)
		if err != nil {
			e.log.WithError(err).Errorf("Failed to get bdev from replica %s with address %s, will skip it and continue", replicaName, replicaAddr)
			e.ReplicaModeMap[replicaName] = types.ModeERR
			e.ReplicaBdevNameMap[replicaName] = ""
			continue
		}
		// TODO: Check if a replica is really a RW replica rather than a rebuilding failed replica
		e.ReplicaModeMap[replicaName] = types.ModeRW
		e.ReplicaBdevNameMap[replicaName] = bdevName
		replicaBdevList = append(replicaBdevList, bdevName)
	}
	e.ReplicaAddressMap = replicaAddressMap
	e.log = e.log.WithField("replicaAddressMap", replicaAddressMap)

	e.CheckAndUpdateInfoFromReplica()

	e.log.Info("Launching RAID during engine creation")
	if _, err := spdkClient.BdevRaidCreate(e.Name, spdktypes.BdevRaidLevel1, 0, replicaBdevList); err != nil {
		return nil, err
	}

	e.log.Info("Launching Frontend during engine creation")
	if err := e.handleFrontend(spdkClient, portCount, superiorPortAllocator); err != nil {
		return nil, err
	}

	e.State = types.InstanceStateRunning

	e.log.Info("Created engine")

	return e.getWithoutLock(), nil
}

func (e *Engine) resizeVolume(spdkClient *spdkclient.Client, size uint64) (err error) {
	e.Lock()
	defer e.Unlock()

	if e.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %s for engine %s resize bdev", e.State, e.Name)
	}

	roundedSize := util.RoundUp(size, helpertypes.MiB)
	if roundedSize != size {
		e.log.Infof("Rounded up spec size from %v to %v since the spec size should be multiple of MiB", size, roundedSize)
	}
	e.log.WithField("specSize", size)

	if e.SpecSize == roundedSize {
		return nil
	}

	if roundedSize < e.ActualSize {
		return fmt.Errorf("cannot shrink the engine %s bdev size from %d to %d", e.Name, e.ActualSize, size)
	}
	
	// Now, we need to resize each replica
	for replicaName, replicaAddress := range e.ReplicaAddressMap {
		if e.ReplicaModeMap[replicaName] != types.ModeRW {
			continue
		}
		if err := e.resizeReplica(spdkClient, replicaName, replicaAddress, roundedSize); err != nil {
			return err
		}
	}
	
	e.SpecSize = roundedSize
	e.log.Infof("Resized engine %s bdev size to %d", e.Name, e.SpecSize)
	return nil
}

func (e *Engine) resizeReplica(spdkClient *spdkclient.Client, replicaName, replicaAddress string, size uint64) (err error) {
	replicaIP, _, err := net.SplitHostPort(replicaAddress)
	if err != nil {
		return err
	}
	if replicaIP == e.IP {
		return nil
	}

	bdevName := e.ReplicaBdevNameMap[replicaName]
	if bdevName == "" {
		return nil
	}
	if _, err := spdkClient.BdevLvolResize(bdevName, size); err != nil {
		return err
	}

	return nil
}

func (e *Engine) getBdevNameForReplica(spdkClient *spdkclient.Client, localReplicaLvsNameMap map[string]string, replicaName, replicaAddress, podIP string) (bdevName string, err error) {
	replicaIP, _, err := net.SplitHostPort(replicaAddress)
	if err != nil {
		return "", err
	}
	if replicaIP == podIP {
		if localReplicaLvsNameMap[replicaName] == "" {
			return "", fmt.Errorf("cannot find local replica %s from the local replica map", replicaName)

		}
		return spdktypes.GetLvolAlias(localReplicaLvsNameMap[replicaName], replicaName), nil
	}

	return e.connectReplica(spdkClient, replicaName, replicaAddress)
}

func (e *Engine) connectReplica(spdkClient *spdkclient.Client, replicaName, replicaAddress string) (bdevName string, err error) {
	replicaIP, replicaPort, err := net.SplitHostPort(replicaAddress)
	if err != nil {
		return "", err
	}
	// This function is not responsible for retrieving the bdev name for local replicas
	if replicaIP == e.IP {
		return "", nil
	}

	nvmeBdevNameList, err := spdkClient.BdevNvmeAttachController(replicaName, helpertypes.GetNQN(replicaName), replicaIP, replicaPort, spdktypes.NvmeTransportTypeTCP, spdktypes.NvmeAddressFamilyIPv4,
		helpertypes.DefaultCtrlrLossTimeoutSec, helpertypes.DefaultReconnectDelaySec, helpertypes.DefaultFastIOFailTimeoutSec)
	if err != nil {
		return "", err
	}
	if len(nvmeBdevNameList) != 1 {
		return "", fmt.Errorf("got zero or multiple results when attaching replica %s with address %s as a NVMe bdev: %+v", replicaName, replicaAddress, nvmeBdevNameList)
	}
	return nvmeBdevNameList[0], nil
}

func (e *Engine) handleFrontend(spdkClient *spdkclient.Client, portCount int32, superiorPortAllocator *util.Bitmap) error {
	if e.Frontend != types.FrontendEmpty && e.Frontend != types.FrontendSPDKTCPNvmf && e.Frontend != types.FrontendSPDKTCPBlockdev {
		return fmt.Errorf("unknown frontend type %s", e.Frontend)
	}

	if e.Frontend == types.FrontendEmpty {
		e.log.Infof("No frontend specified, will not expose the volume %s", e.VolumeName)
		return nil
	}

	nqn := helpertypes.GetNQN(e.Name)

	e.log.Info("Blindly stopping expose bdev for engine")
	if err := spdkClient.StopExposeBdev(nqn); err != nil {
		return errors.Wrap(err, "failed to stop expose bdev for engine")
	}

	port, _, err := superiorPortAllocator.AllocateRange(portCount)
	if err != nil {
		return err
	}
	portStr := strconv.Itoa(int(port))

	if err := spdkClient.StartExposeBdev(nqn, e.Name, e.IP, portStr); err != nil {
		return err
	}
	e.Port = port
	e.log = e.log.WithField("port", port)

	if e.Frontend == types.FrontendSPDKTCPNvmf {
		e.Endpoint = GetNvmfEndpoint(nqn, e.IP, e.Port)
		return nil
	}

	initiator, err := nvme.NewInitiator(e.VolumeName, nqn, nvme.HostProc)
	if err != nil {
		return err
	}
	dmDeviceBusy, err := initiator.Start(e.IP, portStr, true)
	if err != nil {
		return err
	}
	e.dmDeviceBusy = dmDeviceBusy
	e.Endpoint = initiator.GetEndpoint()
	e.log = e.log.WithField("endpoint", e.Endpoint)

	return nil
}

func (e *Engine) Delete(spdkClient *spdkclient.Client, superiorPortAllocator *util.Bitmap) (err error) {
	requireUpdate := false

	e.Lock()
	defer func() {
		// Considering that there may be still pending validations, it's better to update the state after the deletion.
		if err != nil {
			if e.State != types.InstanceStateError {
				e.State = types.InstanceStateError
				e.ErrorMsg = err.Error()
				e.log.WithError(err).Error("Failed to delete engine")
				requireUpdate = true
			}
		} else {
			if e.State != types.InstanceStateError {
				e.ErrorMsg = ""
			}
		}
		if e.State == types.InstanceStateRunning {
			e.State = types.InstanceStateTerminating
			requireUpdate = true
		}

		e.Unlock()

		if requireUpdate {
			e.UpdateCh <- nil
		}
	}()

	if e.Endpoint != "" {
		nqn := helpertypes.GetNQN(e.Name)

		initiator, err := nvme.NewInitiator(e.VolumeName, nqn, nvme.HostProc)
		if err != nil {
			return err
		}
		if _, err := initiator.Stop(true, true, true); err != nil {
			return err
		}

		if err := spdkClient.StopExposeBdev(nqn); err != nil {
			return err
		}

		e.Endpoint = ""
		requireUpdate = true
	}

	if e.Port != 0 {
		if err := superiorPortAllocator.ReleaseRange(e.Port, e.Port); err != nil {
			return err
		}
		e.Port = 0
		requireUpdate = true
	}

	if _, err := spdkClient.BdevRaidDelete(e.Name); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}

	for replicaName := range e.ReplicaAddressMap {
		if err := e.disconnectReplica(spdkClient, replicaName); err != nil {
			if e.ReplicaModeMap[replicaName] != types.ModeERR {
				e.ReplicaModeMap[replicaName] = types.ModeERR
				requireUpdate = true
			}
			return err
		}

		delete(e.ReplicaAddressMap, replicaName)
		delete(e.ReplicaBdevNameMap, replicaName)
		delete(e.ReplicaModeMap, replicaName)
		requireUpdate = true
	}

	e.log.Info("Deleted engine")

	return nil
}

func (e *Engine) disconnectReplica(spdkClient *spdkclient.Client, replicaName string) (err error) {
	replicaIP, _, err := net.SplitHostPort(e.ReplicaAddressMap[replicaName])
	if err != nil {
		return err
	}
	if replicaIP == e.IP {
		return nil
	}

	bdevName := e.ReplicaBdevNameMap[replicaName]
	if bdevName == "" {
		return nil
	}
	if _, err := spdkClient.BdevNvmeDetachController(helperutil.GetNvmeControllerNameFromNamespaceName(bdevName)); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}

	return nil
}

func (e *Engine) Get() (res *spdkrpc.Engine) {
	e.RLock()
	defer e.RUnlock()

	return e.getWithoutLock()
}

func (e *Engine) getWithoutLock() (res *spdkrpc.Engine) {
	res = &spdkrpc.Engine{
		Name:              e.Name,
		SpecSize:          e.SpecSize,
		ActualSize:        e.ActualSize,
		ReplicaAddressMap: e.ReplicaAddressMap,
		ReplicaModeMap:    map[string]spdkrpc.ReplicaMode{},
		Ip:                e.IP,
		Port:              e.Port,
		Snapshots:         map[string]*spdkrpc.Lvol{},
		Frontend:          e.Frontend,
		Endpoint:          e.Endpoint,
		State:             string(e.State),
		ErrorMsg:          e.ErrorMsg,
	}

	for replicaName, replicaMode := range e.ReplicaModeMap {
		res.ReplicaModeMap[replicaName] = spdkrpc.ReplicaModeToGRPCReplicaMode(replicaMode)
	}
	res.Head = api.LvolToProtoLvol(e.Head)
	for snapshotName, snapApiLvol := range e.SnapshotMap {
		res.Snapshots[snapshotName] = api.LvolToProtoLvol(snapApiLvol)
	}

	return res
}

func (e *Engine) ValidateAndUpdate(spdkClient *spdkclient.Client) (err error) {
	updateRequired := false

	e.Lock()
	defer func() {
		e.Unlock()

		if updateRequired {
			e.UpdateCh <- nil
		}
	}()

	if e.IsRestoring {
		e.log.Info("Engine is restoring, will skip the validation and update")
		return nil
	}

	// Syncing with the SPDK TGT server only when the engine is running.
	if e.State != types.InstanceStateRunning {
		return nil
	}

	subsystemMap, err := GetNvmfSubsystemMap(spdkClient)
	if err != nil {
		return err
	}
	bdevMap, err := GetBdevMap(spdkClient)
	if err != nil {
		return err
	}

	defer func() {
		// TODO: we may not need to mark the engine as ERR for each error
		if err != nil {
			if e.State != types.InstanceStateError {
				e.State = types.InstanceStateError
				e.log.WithError(err).Error("Found error during engine validation and update")
				updateRequired = true
			}
			e.ErrorMsg = err.Error()
		} else {
			if e.State != types.InstanceStateError {
				e.ErrorMsg = ""
			}
		}
	}()

	podIP, err := commonNet.GetIPForPod()
	if err != nil {
		return err
	}
	if e.IP != podIP {
		return fmt.Errorf("found mismatching between engine IP %s and pod IP %s for engine %s", e.IP, podIP, e.Name)
	}

	if err := e.validateAndUpdateFrontend(subsystemMap); err != nil {
		return err
	}

	bdevRaid := bdevMap[e.Name]
	if spdktypes.GetBdevType(bdevRaid) != spdktypes.BdevTypeRaid {
		return fmt.Errorf("cannot find a raid bdev for engine %v", e.Name)
	}
	bdevRaidSize := bdevRaid.NumBlocks * uint64(bdevRaid.BlockSize)
	if e.SpecSize != bdevRaidSize {
		return fmt.Errorf("found mismatching between engine spec size %d and actual raid bdev size %d for engine %s", e.SpecSize, bdevRaidSize, e.Name)
	}

	// Verify engine map consistency
	for replicaName := range e.ReplicaAddressMap {
		if _, exists := e.ReplicaBdevNameMap[replicaName]; !exists {
			e.ReplicaBdevNameMap[replicaName] = ""
			e.ReplicaModeMap[replicaName] = types.ModeERR
			e.log.Errorf("Mark replica %s as mode ERR since it is not found in engine %s bdev name map", replicaName, e.Name)
		}
		if _, exists := e.ReplicaModeMap[replicaName]; !exists {
			e.ReplicaModeMap[replicaName] = types.ModeERR
			e.log.Errorf("Mark replica %s as mode ERR since it is not found in engine %s mode map", replicaName, e.Name)
		}
	}
	for replicaName := range e.ReplicaBdevNameMap {
		if _, exists := e.ReplicaAddressMap[replicaName]; !exists {
			e.ReplicaAddressMap[replicaName] = ""
			e.ReplicaModeMap[replicaName] = types.ModeERR
			e.log.Errorf("Mark replica %s as mode ERR since it is not found in engine %s address map", replicaName, e.Name)
		}
		if _, exists := e.ReplicaModeMap[replicaName]; !exists {
			e.ReplicaModeMap[replicaName] = types.ModeERR
			e.log.Errorf("Mark replica %s as mode ERR since it is not found in engine %s mode map", replicaName, e.Name)
		}
	}
	// Now e.ReplicaAddressMap and e.ReplicaBdevNameMap should have the same key set
	for replicaName := range e.ReplicaModeMap {
		if _, exists := e.ReplicaAddressMap[replicaName]; !exists {
			delete(e.ReplicaModeMap, replicaName)
			e.log.Errorf("Remove replica %s for the mode map since it is not found in engine %s address map", replicaName, e.Name)
		}
		if _, exists := e.ReplicaBdevNameMap[replicaName]; !exists {
			delete(e.ReplicaBdevNameMap, replicaName)
			e.log.Errorf("Remove replica %s for the mode map since it is not found in engine %s bdev name map", replicaName, e.Name)
		}
	}
	e.log = e.log.WithField("replicaAddressMap", e.ReplicaAddressMap)

	containValidReplica := false
	for replicaName, bdevName := range e.ReplicaBdevNameMap {
		if e.ReplicaModeMap[replicaName] == types.ModeERR || e.ReplicaModeMap[replicaName] == types.ModeWO {
			continue
		}
		mode, err := e.validateAndUpdateReplicaMode(replicaName, bdevMap[bdevName])
		if err != nil {
			if e.ReplicaModeMap[replicaName] != types.ModeERR {
				e.log.WithError(err).Errorf("Replica %s is invalid, will update the mode from %s to %s", replicaName, e.ReplicaModeMap[replicaName], types.ModeERR)
				e.ReplicaModeMap[replicaName] = types.ModeERR
				updateRequired = true
			}
			continue
		}
		if e.ReplicaModeMap[replicaName] != mode {
			e.log.Debugf("Replica %s mode is updated from %s to %s", replicaName, e.ReplicaModeMap[replicaName], mode)
			e.ReplicaModeMap[replicaName] = mode
			updateRequired = true
		}
		if e.ReplicaModeMap[replicaName] == types.ModeRW {
			containValidReplica = true
		}
	}
	if !containValidReplica {
		e.State = types.InstanceStateError
		updateRequired = true
		// TODO: should we delete the engine automatically here?
	}

	e.CheckAndUpdateInfoFromReplica()

	return nil
}

func (e *Engine) CheckAndUpdateInfoFromReplica() {
	replicaMap := map[string]*api.Replica{}
	replicaAncestorMap := map[string]*api.Lvol{}
	// hasBackingImage := false
	hasSnapshot := false
	for replicaName, address := range e.ReplicaAddressMap {
		if e.ReplicaModeMap[replicaName] != types.ModeRW {
			continue
		}
		replicaServiceCli, err := GetServiceClient(address)
		if err != nil {
			e.log.WithError(err).Warnf("failed to get service client for replica %s with address %s, will skip this replica and continue info update from replica", replicaName, address)
			continue
		}
		replica, err := replicaServiceCli.ReplicaGet(replicaName)
		if err != nil {
			e.log.WithError(err).Warnf("failed to get replica %s with address %s, will skip this replica and continue info update from replica", replicaName, address)
			continue
		}

		// The ancestor check sequence: the backing image, then the oldest snapshot, finally head
		// TODO: Check the backing image first

		// if replica.BackingImage != nil {
		//	hasBackingImage = true
		//	replicaAncestorMap[replicaName] = replica.BackingImage
		// } else
		if len(replica.Snapshots) != 0 {
			// if hasBackingImage {
			//	e.log.Warnf("Found replica %s does not have a backing image while other replicas have during info update from replica", replicaName)
			// } else {}
			hasSnapshot = true
			for snapshotName, snapApiLvol := range replica.Snapshots {
				if snapApiLvol.Parent == "" {
					replicaAncestorMap[replicaName] = replica.Snapshots[snapshotName]
					break
				}
			}
		} else {
			if hasSnapshot {
				e.log.Warnf("Found replica %s does not have a snapshot while other replicas have during info update from replica", replicaName)
			} else {
				replicaAncestorMap[replicaName] = replica.Head
			}
		}
		if replicaAncestorMap[replicaName] == nil {
			e.log.Warnf("Cannot find replica %s ancestor, will skip this replica and continue info update from replica", replicaName)
			continue
		}
		replicaMap[replicaName] = replica
	}

	// If there are multiple candidates, the priority is:
	//  1. the earliest backing image if one replica contains a backing image
	//  2. the earliest snapshot if one replica contains a snapshot
	//  3. the earliest volume head
	candidateReplicaName := ""
	earliestCreationTime := time.Now()
	for replicaName, ancestorApiLvol := range replicaAncestorMap {
		// if hasBackingImage {
		//	if ancestorApiLvol.Name == types.VolumeHead || IsReplicaSnapshotLvol(replicaName, ancestorApiLvol.Name) {
		//		continue
		//	}
		// } else
		if hasSnapshot {
			if ancestorApiLvol.Name == types.VolumeHead {
				continue
			}
		} else {
			if ancestorApiLvol.Name != types.VolumeHead {
				continue
			}
		}

		creationTime, err := time.Parse(time.RFC3339, ancestorApiLvol.CreationTime)
		if err != nil {
			e.log.WithError(err).Warnf("Failed to parse replica %s ancestor creation time, will skip this replica and continue info update from replica: %+v", replicaName, ancestorApiLvol)
			continue
		}
		if earliestCreationTime.After(creationTime) {
			earliestCreationTime = creationTime
			e.SnapshotMap = replicaMap[replicaName].Snapshots
			e.Head = replicaMap[replicaName].Head
			e.ActualSize = replicaMap[replicaName].ActualSize
			if candidateReplicaName != replicaName {
				if candidateReplicaName != "" && replicaAncestorMap[candidateReplicaName].Name != ancestorApiLvol.Name {
					e.log.Warnf("Comparing with replica %s ancestor %s, replica %s has a different and earlier ancestor %s, will update info from this replica", candidateReplicaName, replicaAncestorMap[candidateReplicaName].Name, replicaName, ancestorApiLvol.Name)
				}
				candidateReplicaName = replicaName
			}
		}
	}
}

func (e *Engine) validateAndUpdateFrontend(subsystemMap map[string]*spdktypes.NvmfSubsystem) (err error) {
	if e.Frontend != types.FrontendEmpty && e.Frontend != types.FrontendSPDKTCPNvmf && e.Frontend != types.FrontendSPDKTCPBlockdev {
		return fmt.Errorf("unknown frontend type %s", e.Frontend)
	}

	nqn := helpertypes.GetNQN(e.Name)
	subsystem := subsystemMap[nqn]

	if e.Frontend == types.FrontendEmpty {
		if subsystem != nil {
			return fmt.Errorf("found nvmf subsystem %s for engine %s with empty frontend", nqn, e.Name)
		}
		if e.Endpoint != "" {
			return fmt.Errorf("found non-empty endpoint %s for engine %s with empty frontend", e.Endpoint, e.Name)
		}
		if e.Port != 0 {
			return fmt.Errorf("found non-zero port %v for engine %s with empty frontend", e.Port, e.Name)
		}
		return nil
	}

	if subsystem == nil || len(subsystem.ListenAddresses) == 0 {
		return fmt.Errorf("cannot find the Nvmf subsystem for engine %s", e.Name)
	}

	port := 0
	for _, listenAddr := range subsystem.ListenAddresses {
		if !strings.EqualFold(string(listenAddr.Adrfam), string(spdktypes.NvmeAddressFamilyIPv4)) ||
			!strings.EqualFold(string(listenAddr.Trtype), string(spdktypes.NvmeTransportTypeTCP)) {
			continue
		}
		if port, err = strconv.Atoi(listenAddr.Trsvcid); err != nil {
			return err
		}
		if e.Port == int32(port) {
			break
		}
	}
	if port == 0 || e.Port != int32(port) {
		return fmt.Errorf("cannot find a matching listener with port %d from Nvmf subsystem for engine %s", e.Port, e.Name)
	}

	switch e.Frontend {
	case types.FrontendSPDKTCPBlockdev:
		initiator, err := nvme.NewInitiator(e.VolumeName, nqn, nvme.HostProc)
		if err != nil {
			return err
		}
		if err := initiator.LoadNVMeDeviceInfo(); err != nil {
			if strings.Contains(err.Error(), "connecting state") ||
				strings.Contains(err.Error(), "resetting state") {
				e.log.WithError(err).Warnf("Ignored to validate and update engine %v, because the device is still in a transient state", e.Name)
				return nil
			}
			return err
		}
		if err := initiator.LoadEndpoint(e.dmDeviceBusy); err != nil {
			return err
		}
		blockDevEndpoint := initiator.GetEndpoint()
		if e.Endpoint == "" {
			e.Endpoint = blockDevEndpoint
		}
		if e.Endpoint != blockDevEndpoint {
			return fmt.Errorf("found mismatching between engine endpoint %s and actual block device endpoint %s for engine %s", e.Endpoint, blockDevEndpoint, e.Name)
		}
	case types.FrontendSPDKTCPNvmf:
		nvmfEndpoint := GetNvmfEndpoint(nqn, e.IP, e.Port)
		if e.Endpoint == "" {
			e.Endpoint = nvmfEndpoint
		}
		if e.Endpoint != "" && e.Endpoint != nvmfEndpoint {
			return fmt.Errorf("found mismatching between engine endpoint %s and actual nvmf endpoint %s for engine %s", e.Endpoint, nvmfEndpoint, e.Name)
		}
	default:
		return fmt.Errorf("unknown frontend type %s", e.Frontend)
	}

	return nil
}

func (e *Engine) validateAndUpdateReplicaMode(replicaName string, bdev *spdktypes.BdevInfo) (mode types.Mode, err error) {
	if bdev == nil {
		return types.ModeERR, fmt.Errorf("cannot find a bdev for replica %s", replicaName)
	}
	bdevSpecSize := bdev.NumBlocks * uint64(bdev.BlockSize)
	if e.SpecSize != bdevSpecSize {
		return types.ModeERR, fmt.Errorf("found mismatching between replica %s bdev spec size %d and the engine spec size %d for engine %s", replicaName, bdevSpecSize, e.SpecSize, e.Name)
	}
	switch spdktypes.GetBdevType(bdev) {
	case spdktypes.BdevTypeLvol:
		replicaIP, _, err := net.SplitHostPort(e.ReplicaAddressMap[replicaName])
		if err != nil {
			return types.ModeERR, err
		}
		if e.IP != replicaIP {
			return types.ModeERR, fmt.Errorf("found mismatching between replica %s IP %s and the engine IP %s for engine %s", replicaName, replicaIP, e.IP, e.Name)
		}
	case spdktypes.BdevTypeNvme:
		if len(*bdev.DriverSpecific.Nvme) != 1 {
			return types.ModeERR, fmt.Errorf("found zero or multiple nvme info in a remote nvme base bdev %v for replica %s", bdev.Name, replicaName)
		}
		nvmeInfo := (*bdev.DriverSpecific.Nvme)[0]
		if !strings.EqualFold(string(nvmeInfo.Trid.Adrfam), string(spdktypes.NvmeAddressFamilyIPv4)) ||
			!strings.EqualFold(string(nvmeInfo.Trid.Trtype), string(spdktypes.NvmeTransportTypeTCP)) {
			return types.ModeERR, fmt.Errorf("found invalid address family %s and transport type %s in a remote nvme base bdev %s for replica %s", nvmeInfo.Trid.Adrfam, nvmeInfo.Trid.Trtype, bdev.Name, replicaName)
		}
		bdevAddr := net.JoinHostPort(nvmeInfo.Trid.Traddr, nvmeInfo.Trid.Trsvcid)
		if e.ReplicaAddressMap[replicaName] != bdevAddr {
			return types.ModeERR, fmt.Errorf("found mismatching between replica %s bdev address %s and the nvme bdev actual address %s", replicaName, e.ReplicaAddressMap[replicaName], bdevAddr)
		}
		// TODO: Validate NVMe controller state
		// TODO: Verify Mode WO
	default:
		return types.ModeERR, fmt.Errorf("found invalid bdev type %v for replica %s ", spdktypes.GetBdevType(bdev), replicaName)
	}

	return types.ModeRW, nil
}

func (e *Engine) ReplicaAddStart(spdkClient *spdkclient.Client, replicaName, replicaAddress string) (err error) {
	updateRequired := false

	e.Lock()
	defer func() {
		e.Unlock()

		if updateRequired {
			e.UpdateCh <- nil
		}
	}()

	// Syncing with the SPDK TGT server only when the engine is running.
	if e.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %v for engine %s replica %s add start", e.State, e.Name, replicaName)
	}

	if e.Frontend != types.FrontendEmpty {
		return fmt.Errorf("invalid frontend %v for engine %s replica %s add start", e.Frontend, e.Name, replicaName)
	}

	if _, exists := e.ReplicaAddressMap[replicaName]; exists {
		return fmt.Errorf("replica %s already exists", replicaName)
	}

	replicaClients, err := e.getReplicaClients()
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			if e.State != types.InstanceStateError {
				e.State = types.InstanceStateError
				updateRequired = true
			}
			e.ErrorMsg = err.Error()
		} else {
			if e.State != types.InstanceStateError {
				e.ErrorMsg = ""
			}
		}
	}()

	// TODO: For online rebuilding, the IO should be paused first
	snapshotName := GenerateRebuildingSnapshotName()
	updateRequired, err = e.snapshotOperationWithoutLock(spdkClient, replicaClients, snapshotName, SnapshotOperationCreate)
	if err != nil {
		return err
	}

	// TODO: For online rebuilding, this replica should be attached (if it's a remote one) then added to the RAID base bdev list with mode WO
	e.ReplicaAddressMap[replicaName] = replicaAddress
	e.ReplicaBdevNameMap[replicaName] = ""
	e.ReplicaModeMap[replicaName] = types.ModeWO
	e.log = e.log.WithField("replicaAddressMap", e.ReplicaAddressMap)

	return nil
}

func (e *Engine) ReplicaAddFinish(spdkClient *spdkclient.Client, replicaName, replicaAddress string, localReplicaLvsNameMap map[string]string) (err error) {
	updateRequired := false

	e.Lock()
	defer func() {
		e.Unlock()

		if updateRequired {
			e.UpdateCh <- nil
		}
	}()

	// Syncing with the SPDK TGT server only when the engine is running.
	if e.State != types.InstanceStateRunning {
		return fmt.Errorf("invalid state %v for engine %s replica %s add finish", e.State, e.Name, replicaName)
	}

	if e.Frontend != types.FrontendEmpty {
		return fmt.Errorf("invalid frontend %v for engine %s replica %s add finish", e.Frontend, e.Name, replicaName)
	}

	if _, exists := e.ReplicaAddressMap[replicaName]; !exists {
		return fmt.Errorf("replica %s does not exist in engine %s", replicaName, e.Name)
	}

	if e.ReplicaModeMap[replicaName] != types.ModeWO {
		return fmt.Errorf("invalid mode %s for engine %s replica %s add finish", e.ReplicaModeMap[replicaName], e.Name, replicaName)
	}

	replicaBdevList := []string{}
	for rName, rMode := range e.ReplicaModeMap {
		if rMode == types.ModeRW {
			replicaBdevList = append(replicaBdevList, e.ReplicaBdevNameMap[rName])
		}
		if rName == replicaName {
			bdevName, err := e.getBdevNameForReplica(spdkClient, localReplicaLvsNameMap, replicaName, replicaAddress, e.IP)
			if err != nil {
				e.log.WithError(err).Errorf("Failed to get bdev from rebuilding replica %s with address %s, will mark it as ERR and error out", replicaName, replicaAddress)
				e.ReplicaModeMap[replicaName] = types.ModeERR
				e.ReplicaBdevNameMap[replicaName] = ""
				updateRequired = true
				return err
			}
			e.ReplicaModeMap[replicaName] = types.ModeRW
			e.ReplicaBdevNameMap[replicaName] = bdevName
			replicaBdevList = append(replicaBdevList, bdevName)
			updateRequired = true
		}
	}

	defer func() {
		if err != nil {
			if e.State != types.InstanceStateError {
				e.State = types.InstanceStateError
				updateRequired = true
			}
			e.ErrorMsg = err.Error()
		} else {
			if e.State != types.InstanceStateError {
				e.ErrorMsg = ""
			}
		}
	}()

	if _, err := spdkClient.BdevRaidDelete(e.Name); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}
	if _, err := spdkClient.BdevRaidCreate(e.Name, spdktypes.BdevRaidLevel1, 0, replicaBdevList); err != nil {
		return err
	}

	return nil
}

func (e *Engine) ReplicaShallowCopy(dstReplicaName, dstReplicaAddress string) (err error) {
	e.RLock()

	// Syncing with the SPDK TGT server only when the engine is running.
	if e.State != types.InstanceStateRunning {
		e.RUnlock()
		return fmt.Errorf("invalid state %v for engine %s replica %s shallow copy", e.State, e.Name, dstReplicaName)
	}
	if e.Frontend != types.FrontendEmpty {
		e.RUnlock()
		return fmt.Errorf("invalid frontend %v for engine %s replica %s shallow copy", e.Frontend, e.Name, dstReplicaName)
	}

	srcReplicaName, srcReplicaAddress := "", ""
	for replicaName, replicaMode := range e.ReplicaModeMap {
		if replicaMode != types.ModeRW {
			continue
		}
		srcReplicaName = replicaName
		srcReplicaAddress = e.ReplicaAddressMap[replicaName]
		break
	}
	e.RUnlock()

	if srcReplicaName == "" || srcReplicaAddress == "" {
		return fmt.Errorf("cannot find an RW replica for engine %s replica %s shallow copy", e.Name, dstReplicaName)
	}

	// TODO: Can we share the clients in the whole server?
	srcReplicaServiceCli, err := GetServiceClient(srcReplicaAddress)
	if err != nil {
		return err
	}
	dstReplicaServiceCli, err := GetServiceClient(dstReplicaAddress)
	if err != nil {
		return err
	}

	srcReplicaIP, _, _ := net.SplitHostPort(srcReplicaAddress)
	dstReplicaIP, _, _ := net.SplitHostPort(dstReplicaAddress)

	rpcSrcReplica, err := srcReplicaServiceCli.ReplicaGet(srcReplicaName)
	if err != nil {
		return err
	}

	ancestorSnapshotName, latestSnapshotName := "", ""
	for snapshotName, snapApiLvol := range rpcSrcReplica.Snapshots {
		if snapApiLvol.Parent == "" {
			ancestorSnapshotName = snapshotName
		}
		if snapApiLvol.Children[types.VolumeHead] {
			latestSnapshotName = snapshotName
		}
	}
	if ancestorSnapshotName == "" || latestSnapshotName == "" {
		return fmt.Errorf("cannot find the ancestor snapshot %s or latest snapshot %s from RW replica %s snapshot map during engine %s replica %s shallow copy", ancestorSnapshotName, latestSnapshotName, srcReplicaName, e.Name, dstReplicaName)
	}

	defer func() {
		// Blindly mark the rebuilding replica as mode ERR now.
		if err != nil {
			// Blindly send rebuilding finish for src replica.
			if srcReplicaErr := srcReplicaServiceCli.ReplicaRebuildingSrcFinish(srcReplicaName, dstReplicaName); srcReplicaErr != nil {
				e.log.WithError(srcReplicaErr).Errorf("Failed to finish rebuilding for src replica %s with address %s after rebuilding failure, will do nothing", srcReplicaName, srcReplicaAddress)
			}
			e.Lock()
			if e.ReplicaModeMap[dstReplicaName] != types.ModeERR {
				e.ReplicaModeMap[dstReplicaName] = types.ModeERR
				e.log.WithError(err).Errorf("Failed to rebuild replica %s with address %s from src replica %s with address %s, will mark the rebuilding replica mode as ERR", dstReplicaName, dstReplicaAddress, srcReplicaName, srcReplicaAddress)
			}
			e.Unlock()
		}
	}()

	dstRebuildingLvolAddress, err := dstReplicaServiceCli.ReplicaRebuildingDstStart(dstReplicaName, srcReplicaIP != dstReplicaIP)
	if err != nil {
		return err
	}
	if err = srcReplicaServiceCli.ReplicaRebuildingSrcStart(srcReplicaName, dstReplicaName, dstRebuildingLvolAddress); err != nil {
		return err
	}

	// Traverse the src replica snapshot tree with a DFS way and do shallow copy one by one
	rebuildingSnapshotList := []string{}
	rebuildingSnapshotList = getRebuildingSnapshotList(rpcSrcReplica, ancestorSnapshotName, rebuildingSnapshotList)
	currentSnapshotName, prevSnapshotName := "", ""
	for idx := 0; idx < len(rebuildingSnapshotList); idx++ {
		currentSnapshotName = rebuildingSnapshotList[idx]
		if prevSnapshotName != "" && rpcSrcReplica.Snapshots[currentSnapshotName].Parent != prevSnapshotName {
			if err = srcReplicaServiceCli.ReplicaRebuildingSrcDetach(srcReplicaName, dstReplicaName); err != nil {
				return err
			}
			if err = dstReplicaServiceCli.ReplicaRebuildingDstSnapshotRevert(dstReplicaName, rpcSrcReplica.Snapshots[currentSnapshotName].Parent); err != nil {
				return err
			}
			if err = srcReplicaServiceCli.ReplicaRebuildingSrcAttach(srcReplicaName, dstReplicaName, dstRebuildingLvolAddress); err != nil {
				return err
			}
		}
		if err = srcReplicaServiceCli.ReplicaSnapshotShallowCopy(srcReplicaName, currentSnapshotName); err != nil {
			return err
		}
		if err = dstReplicaServiceCli.ReplicaRebuildingDstSnapshotCreate(dstReplicaName, currentSnapshotName); err != nil {
			return err
		}
		prevSnapshotName = currentSnapshotName
	}

	// TODO: The rebuilding lvol of the dst replica is actually the head. Need to make sure the head stands behind to the correct snapshot.
	//  Once we start to use a separate rebuilding lvol rather than the head, we can remove the below code.
	if !rpcSrcReplica.Snapshots[prevSnapshotName].Children[types.VolumeHead] {
		for snapshotName, snapApiLvol := range rpcSrcReplica.Snapshots {
			if !snapApiLvol.Children[types.VolumeHead] {
				continue
			}
			if err = srcReplicaServiceCli.ReplicaRebuildingSrcDetach(srcReplicaName, dstReplicaName); err != nil {
				return err
			}
			if err = dstReplicaServiceCli.ReplicaRebuildingDstSnapshotRevert(dstReplicaName, snapshotName); err != nil {
				return err
			}
			break
		}
	}

	if err = srcReplicaServiceCli.ReplicaRebuildingSrcFinish(srcReplicaName, dstReplicaName); err != nil {
		return err
	}
	// TODO: Connect the previously found latest snapshot lvol with the head lvol chain
	// TODO: For online rebuilding, the rebuilding lvol must be unexposed
	if err = dstReplicaServiceCli.ReplicaRebuildingDstFinish(dstReplicaName, e.IP == dstReplicaIP); err != nil {
		return err
	}

	return nil
}

func getRebuildingSnapshotList(rpcSrcReplica *api.Replica, currentSnapshotName string, rebuildingSnapshotList []string) []string {
	if currentSnapshotName == "" || currentSnapshotName == types.VolumeHead {
		return rebuildingSnapshotList
	}
	rebuildingSnapshotList = append(rebuildingSnapshotList, currentSnapshotName)
	for childSnapshotName := range rpcSrcReplica.Snapshots[currentSnapshotName].Children {
		rebuildingSnapshotList = getRebuildingSnapshotList(rpcSrcReplica, childSnapshotName, rebuildingSnapshotList)
	}
	return rebuildingSnapshotList
}

func (e *Engine) ReplicaDelete(spdkClient *spdkclient.Client, replicaName, replicaAddress string) (err error) {
	e.Lock()
	defer e.Unlock()

	if replicaName == "" {
		for rName, rAddr := range e.ReplicaAddressMap {
			if rAddr == replicaAddress {
				replicaName = rName
				break
			}
		}
	}
	if replicaName == "" {
		return fmt.Errorf("cannot find replica name with address %s for engine %s replica delete", replicaAddress, e.Name)
	}
	if e.ReplicaAddressMap[replicaName] == "" {
		return fmt.Errorf("cannot find replica %s for engine %s replica delete", replicaName, e.Name)
	}
	if replicaAddress != "" && e.ReplicaAddressMap[replicaName] != replicaAddress {
		return fmt.Errorf("replica %s recorded address %s does not match the input address %s for engine %s replica delete", replicaName, e.ReplicaAddressMap[replicaName], replicaAddress, e.Name)
	}

	if _, err := spdkClient.BdevRaidRemoveBaseBdev(e.ReplicaBdevNameMap[replicaName]); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return err
	}

	delete(e.ReplicaAddressMap, replicaName)
	delete(e.ReplicaModeMap, replicaName)
	delete(e.ReplicaBdevNameMap, replicaName)
	e.log = e.log.WithField("replicaAddressMap", e.ReplicaAddressMap)

	return nil
}

type SnapshotOperationType string

const (
	SnapshotOperationCreate = SnapshotOperationType("snapshot-create")
	SnapshotOperationDelete = SnapshotOperationType("snapshot-delete")
	SnapshotOperationRevert = SnapshotOperationType("snapshot-revert")
)

func (e *Engine) SnapshotCreate(spdkClient *spdkclient.Client, inputSnapshotName string) (snapshotName string, err error) {
	return e.snapshotOperation(spdkClient, inputSnapshotName, SnapshotOperationCreate)
}

func (e *Engine) SnapshotDelete(spdkClient *spdkclient.Client, snapshotName string) (err error) {
	_, err = e.snapshotOperation(spdkClient, snapshotName, SnapshotOperationDelete)
	return err
}

func (e *Engine) SnapshotRevert(spdkClient *spdkclient.Client, snapshotName string) (err error) {
	_, err = e.snapshotOperation(spdkClient, snapshotName, SnapshotOperationRevert)
	return err
}

func (e *Engine) snapshotOperation(spdkClient *spdkclient.Client, inputSnapshotName string, snapshotOp SnapshotOperationType) (snapshotName string, err error) {
	updateRequired := false

	if snapshotOp == SnapshotOperationCreate {
		e.RLock()
		devicePath := ""
		if e.State == types.InstanceStateRunning && e.Frontend == types.FrontendSPDKTCPBlockdev {
			devicePath = e.Endpoint
		}
		e.RUnlock()
		if devicePath != "" {
			ne, err := helperutil.NewExecutor(commonTypes.HostProcDirectory)
			if err != nil {
				e.log.WithError(err).Errorf("WARNING: failed to get the executor for snapshot op %v with snapshot %s, will skip the sync and continue", snapshotOp, inputSnapshotName)
			} else {
				e.log.Infof("Requesting system sync %v before snapshot", devicePath)
				// TODO: only sync the device path rather than all filesystems
				if _, err := ne.Execute(nil, "sync", []string{}, SyncTimeout); err != nil {
					// sync should never fail though, so it more like due to the nsenter
					e.log.WithError(err).Errorf("WARNING: failed to sync for snapshot op %v with snapshot %s, will skip the sync and continue", snapshotOp, inputSnapshotName)
				}
			}
		}
	}

	e.Lock()
	defer func() {
		e.Unlock()

		if updateRequired {
			e.UpdateCh <- nil
		}
	}()

	// Syncing with the SPDK TGT server only when the engine is running.
	if e.State != types.InstanceStateRunning {
		return "", fmt.Errorf("invalid state %v for engine %s snapshot %s operation", e.State, e.Name, inputSnapshotName)
	}

	replicaClients, err := e.getReplicaClients()
	if err != nil {
		return "", err
	}

	if snapshotName, err = e.snapshotOperationPreCheckWithoutLock(replicaClients, inputSnapshotName, snapshotOp); err != nil {
		return "", err
	}

	defer func() {
		if err != nil {
			if e.State != types.InstanceStateError {
				e.State = types.InstanceStateError
				updateRequired = true
			}
			e.ErrorMsg = err.Error()
		} else {
			if e.State != types.InstanceStateError {
				e.ErrorMsg = ""
			}
		}
	}()

	if updateRequired, err = e.snapshotOperationWithoutLock(spdkClient, replicaClients, snapshotName, snapshotOp); err != nil {
		return "", err
	}

	e.CheckAndUpdateInfoFromReplica()

	e.log.Infof("Engine finished snapshot %s for %v", snapshotOp, snapshotName)

	return snapshotName, nil
}

func (e *Engine) getReplicaClients() (replicaClients map[string]*client.SPDKClient, err error) {
	replicaClients = map[string]*client.SPDKClient{}
	for replicaName := range e.ReplicaAddressMap {
		if e.ReplicaModeMap[replicaName] == types.ModeERR {
			continue
		}
		c, err := GetServiceClient(e.ReplicaAddressMap[replicaName])
		if err != nil {
			return nil, err
		}
		replicaClients[replicaName] = c
	}

	return replicaClients, nil
}

func (e *Engine) snapshotOperationPreCheckWithoutLock(replicaClients map[string]*client.SPDKClient, snapshotName string, snapshotOp SnapshotOperationType) (string, error) {
	for replicaName := range replicaClients {
		switch snapshotOp {
		case SnapshotOperationCreate:
			if snapshotName == "" {
				snapshotName = util.UUID()[:8]
			}
		case SnapshotOperationDelete:
			if snapshotName == "" {
				return "", fmt.Errorf("empty snapshot name for engine %s snapshot deletion", e.Name)
			}
			if e.ReplicaModeMap[replicaName] == types.ModeWO {
				return "", fmt.Errorf("engine %s contains WO replica %s during snapshot %s delete", e.Name, replicaName, snapshotName)
			}
			e.CheckAndUpdateInfoFromReplica()
			if len(e.SnapshotMap[snapshotName].Children) > 1 {
				return "", fmt.Errorf("engine %s cannot delete snapshot %s since it contains multiple children %+v", e.Name, snapshotName, e.SnapshotMap[snapshotName].Children)
			}
			// TODO: SPDK allows deleting the parent of the volume head. To make the behavior consistent between v1 and v2 engines, we manually disable if for now.
			for childName := range e.SnapshotMap[snapshotName].Children {
				if childName == types.VolumeHead {
					return "", fmt.Errorf("engine %s cannot delete snapshot %s since it is the parent of volume head", e.Name, snapshotName)
				}
			}
		case SnapshotOperationRevert:
			if snapshotName == "" {
				return "", fmt.Errorf("empty snapshot name for engine %s snapshot deletion", e.Name)
			}
			if e.Frontend != types.FrontendEmpty {
				return "", fmt.Errorf("invalid frontend %v for engine %s snapshot %s revert", e.Frontend, e.Name, snapshotName)
			}
			if e.ReplicaModeMap[replicaName] == types.ModeWO {
				return "", fmt.Errorf("engine %s contains WO replica %s during snapshot %s revert", e.Name, replicaName, snapshotName)
			}
			r, err := replicaClients[replicaName].ReplicaGet(replicaName)
			if err != nil {
				return "", err
			}
			if r.Snapshots[snapshotName] == nil {
				return "", fmt.Errorf("replica %s does not contain the reverting snapshot %s", replicaName, snapshotName)
			}
		default:
			return "", fmt.Errorf("unknown replica snapshot operation %s", snapshotOp)
		}
	}

	return snapshotName, nil
}

func (e *Engine) snapshotOperationWithoutLock(spdkClient *spdkclient.Client, replicaClients map[string]*client.SPDKClient, snapshotName string, snapshotOp SnapshotOperationType) (updated bool, err error) {
	if snapshotOp == SnapshotOperationRevert {
		if _, err := spdkClient.BdevRaidDelete(e.Name); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			e.log.WithError(err).Errorf("Failed to delete RAID after snapshot %s revert", snapshotName)
			return false, err
		}
	}

	for replicaName := range replicaClients {
		if err := e.replicaSnapshotOperation(spdkClient, replicaClients[replicaName], replicaName, snapshotName, snapshotOp); err != nil && e.ReplicaModeMap[replicaName] != types.ModeERR {
			e.ReplicaModeMap[replicaName] = types.ModeERR
			e.ReplicaBdevNameMap[replicaName] = ""
			e.log.WithError(err).Errorf("Failed to issue operation %s for replica %s snapshot %s, will mark the replica as mode ERR", snapshotOp, replicaName, snapshotName)
			updated = true
		}
	}

	if snapshotOp == SnapshotOperationRevert {
		replicaBdevList := []string{}
		for replicaName, bdevName := range e.ReplicaBdevNameMap {
			if e.ReplicaModeMap[replicaName] != types.ModeRW {
				continue
			}
			if e.ReplicaBdevNameMap[replicaName] == "" {
				continue
			}
			replicaBdevList = append(replicaBdevList, bdevName)
		}
		if _, err := spdkClient.BdevRaidCreate(e.Name, spdktypes.BdevRaidLevel1, 0, replicaBdevList); err != nil {
			e.log.WithError(err).Errorf("Failed to re-create RAID after snapshot %s revert", snapshotName)
			return false, err
		}
	}

	return updated, nil
}

func (e *Engine) replicaSnapshotOperation(spdkClient *spdkclient.Client, replicaClient *client.SPDKClient, replicaName, snapshotName string, snapshotOp SnapshotOperationType) error {
	switch snapshotOp {
	case SnapshotOperationCreate:
		// TODO: execute `sync` for the nvme initiator before snapshot start
		return replicaClient.ReplicaSnapshotCreate(replicaName, snapshotName)
	case SnapshotOperationDelete:
		return replicaClient.ReplicaSnapshotDelete(replicaName, snapshotName)
	case SnapshotOperationRevert:
		if err := e.disconnectReplica(spdkClient, replicaName); err != nil {
			return err
		}
		if err := replicaClient.ReplicaSnapshotRevert(replicaName, snapshotName); err != nil {
			return err
		}
		bdevName, err := e.connectReplica(spdkClient, replicaName, e.ReplicaAddressMap[replicaName])
		if err != nil {
			return err
		}
		if bdevName != "" {
			e.ReplicaBdevNameMap[replicaName] = bdevName
		}
	default:
		return fmt.Errorf("unknown replica snapshot operation %s", snapshotOp)
	}

	return nil
}

func (e *Engine) ReplicaList(spdkClient *spdkclient.Client) (ret map[string]*api.Replica, err error) {
	e.Lock()
	defer e.Unlock()

	replicas := map[string]*api.Replica{}

	for name, address := range e.ReplicaAddressMap {
		replicaServiceCli, err := GetServiceClient(address)
		if err != nil {
			e.log.WithError(err).Errorf("Failed to get service client for replica %s with address %s", name, address)
			continue
		}

		replica, err := replicaServiceCli.ReplicaGet(name)
		if err != nil {
			e.log.WithError(err).Errorf("Failed to get replica %s with address %s", name, address)
			continue
		}

		replicas[name] = replica
	}

	return replicas, nil
}

func (e *Engine) SetErrorState() {
	needUpdate := false

	e.Lock()
	defer func() {
		e.Unlock()

		if needUpdate {
			e.UpdateCh <- nil
		}
	}()

	if e.State != types.InstanceStateStopped && e.State != types.InstanceStateError {
		e.State = types.InstanceStateError
		needUpdate = true
	}
}

func (e *Engine) BackupCreate(backupName, volumeName, engineName, snapshotName, backingImageName, backingImageChecksum string,
	labels []string, backupTarget string, credential map[string]string, concurrentLimit int32, compressionMethod, storageClassName string, size uint64) (*BackupCreateInfo, error) {

	e.Lock()
	defer e.Unlock()

	replicaName, replicaAddress := "", ""
	for name, mode := range e.ReplicaModeMap {
		if mode != types.ModeRW {
			continue
		}
		replicaName = name
		replicaAddress = e.ReplicaAddressMap[name]
		break
	}

	e.log.Infof("Creating backup %s for volume %s on replica %s address %s", backupName, volumeName, replicaName, replicaAddress)

	replicaServiceCli, err := GetServiceClient(replicaAddress)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, err.Error())
	}

	recv, err := replicaServiceCli.ReplicaBackupCreate(&client.BackupCreateRequest{
		BackupName:           backupName,
		SnapshotName:         snapshotName,
		VolumeName:           volumeName,
		ReplicaName:          replicaName,
		Size:                 size,
		BackupTarget:         backupTarget,
		StorageClassName:     storageClassName,
		BackingImageName:     backingImageName,
		BackingImageChecksum: backingImageChecksum,
		CompressionMethod:    compressionMethod,
		ConcurrentLimit:      concurrentLimit,
		Labels:               labels,
		Credential:           credential,
	})
	if err != nil {
		return nil, err
	}
	return &BackupCreateInfo{
		BackupName:     recv.Backup,
		IsIncremental:  recv.IsIncremental,
		ReplicaAddress: replicaAddress,
	}, nil
}

func (e *Engine) BackupStatus(backupName, replicaAddress string) (*spdkrpc.BackupStatusResponse, error) {
	e.Lock()
	defer e.Unlock()

	found := false
	for name, mode := range e.ReplicaModeMap {
		if e.ReplicaAddressMap[name] == replicaAddress {
			if mode != types.ModeRW {
				return nil, grpcstatus.Errorf(grpccodes.Internal, "replica %s is not in RW mode", name)
			}
			found = true
			break
		}
	}

	if !found {
		return nil, grpcstatus.Errorf(grpccodes.NotFound, fmt.Sprintf("replica address %s is not found in engine %s for getting backup %v status", replicaAddress, e.Name, backupName))
	}

	replicaServiceCli, err := GetServiceClient(replicaAddress)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, err.Error())
	}

	return replicaServiceCli.ReplicaBackupStatus(backupName)
}

func (e *Engine) BackupRestore(spdkClient *spdkclient.Client, backupUrl, engineName, snapshotName string, credential map[string]string, concurrentLimit int32) (*spdkrpc.EngineBackupRestoreResponse, error) {
	e.Lock()
	defer e.Unlock()

	e.log.Infof("Deleting raid bdev %s before restoration", e.Name)
	if _, err := spdkClient.BdevRaidDelete(e.Name); err != nil && !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
		return nil, errors.Wrapf(err, "failed to delete raid bdev %s before restoration", e.Name)
	}

	e.log.Info("Disconnecting all replicas before restoration")
	for replicaName := range e.ReplicaAddressMap {
		if err := e.disconnectReplica(spdkClient, replicaName); err != nil {
			e.log.Infof("Failed to remove replica %s before restoration", replicaName)
			return nil, errors.Wrapf(err, "failed to remove replica %s before restoration", replicaName)
		}
	}

	e.IsRestoring = true

	// TODO: support DR volume
	if len(e.SnapshotMap) == 0 {
		if snapshotName == "" {
			snapshotName = util.UUID()
			e.log.Infof("Generating a snapshot name %s for the full restore", snapshotName)
		}
	} else {
		return nil, errors.Errorf("incremental restore is not supported yet")
	}

	resp := &spdkrpc.EngineBackupRestoreResponse{
		Errors: map[string]string{},
	}
	for replicaName, replicaAddress := range e.ReplicaAddressMap {
		e.log.Infof("Restoring backup on replica %s address %s", replicaName, replicaAddress)

		replicaServiceCli, err := GetServiceClient(replicaAddress)
		if err != nil {
			e.log.WithError(err).Errorf("Failed to restore backup on replica %s address %s", replicaName, replicaAddress)
			resp.Errors[replicaAddress] = err.Error()
			continue
		}

		err = replicaServiceCli.ReplicaBackupRestore(&client.BackupRestoreRequest{
			BackupUrl:       backupUrl,
			ReplicaName:     replicaName,
			SnapshotName:    snapshotName,
			Credential:      credential,
			ConcurrentLimit: concurrentLimit,
		})
		if err != nil {
			e.log.WithError(err).Errorf("Failed to restore backup on replica %s address %s", replicaName, replicaAddress)
			resp.Errors[replicaAddress] = err.Error()
		}
	}

	return resp, nil
}

func (e *Engine) BackupRestoreFinish(spdkClient *spdkclient.Client) error {
	e.Lock()
	defer e.Unlock()

	replicaBdevList := []string{}
	for replicaName, bdevName := range e.ReplicaBdevNameMap {
		replicaAddress := e.ReplicaAddressMap[replicaName]
		replicaIP, replicaPort, err := net.SplitHostPort(replicaAddress)
		if err != nil {
			return err
		}
		if replicaIP == e.IP {
			replicaBdevList = append(replicaBdevList, bdevName)
			continue
		}
		e.log.Infof("Attaching replica %s with address %s before finishing restoration", replicaName, replicaAddress)
		_, err = spdkClient.BdevNvmeAttachController(replicaName, helpertypes.GetNQN(replicaName), replicaIP, replicaPort, spdktypes.NvmeTransportTypeTCP, spdktypes.NvmeAddressFamilyIPv4,
			helpertypes.DefaultCtrlrLossTimeoutSec, helpertypes.DefaultReconnectDelaySec, helpertypes.DefaultFastIOFailTimeoutSec)
		if err != nil {
			return err
		}
		replicaBdevList = append(replicaBdevList, bdevName)
	}

	e.log.Infof("Creating raid bdev %s with replicas %+v before finishing restoration", e.Name, replicaBdevList)
	if _, err := spdkClient.BdevRaidCreate(e.Name, spdktypes.BdevRaidLevel1, 0, replicaBdevList); err != nil {
		if !jsonrpc.IsJSONRPCRespErrorFileExists(err) {
			e.log.WithError(err).Errorf("Failed to create raid bdev before finishing restoration")
			return err
		}
	}

	e.IsRestoring = false

	return nil
}

func (e *Engine) RestoreStatus() (*spdkrpc.RestoreStatusResponse, error) {
	resp := &spdkrpc.RestoreStatusResponse{
		Status: map[string]*spdkrpc.ReplicaRestoreStatusResponse{},
	}

	e.Lock()
	defer e.Unlock()

	for replicaName, replicaAddress := range e.ReplicaAddressMap {
		if e.ReplicaModeMap[replicaName] != types.ModeRW {
			continue
		}

		replicaServiceCli, err := GetServiceClient(replicaAddress)
		if err != nil {
			return nil, err
		}
		status, err := replicaServiceCli.ReplicaRestoreStatus(replicaName)
		if err != nil {
			return nil, err
		}
		resp.Status[replicaAddress] = status
	}

	return resp, nil
}
