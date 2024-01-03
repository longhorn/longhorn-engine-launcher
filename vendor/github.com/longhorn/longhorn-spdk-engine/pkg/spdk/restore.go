package spdk

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/longhorn/backupstore"
	btypes "github.com/longhorn/backupstore/types"
	butil "github.com/longhorn/backupstore/util"
	commonNs "github.com/longhorn/go-common-libs/ns"
	commonTypes "github.com/longhorn/go-common-libs/types"

	"github.com/longhorn/go-spdk-helper/pkg/nvme"
	helperutil "github.com/longhorn/go-spdk-helper/pkg/util"

	spdkclient "github.com/longhorn/go-spdk-helper/pkg/spdk/client"
	helpertypes "github.com/longhorn/go-spdk-helper/pkg/types"
)

type Restore struct {
	sync.RWMutex

	spdkClient *spdkclient.Client
	replica    *Replica

	Progress  int
	Error     string
	BackupURL string
	State     btypes.ProgressState

	// The snapshot file that stores the restored data in the end.
	LvolName string

	LastRestored           string
	CurrentRestoringBackup string

	ip             string
	port           int32
	executor       *commonNs.Executor
	subsystemNQN   string
	controllerName string
	initiator      *nvme.Initiator

	stopOnce sync.Once
	stopChan chan struct{}

	log logrus.FieldLogger
}

func NewRestore(spdkClient *spdkclient.Client, lvolName, backupUrl, backupName string, replica *Replica) (*Restore, error) {
	log := logrus.WithFields(logrus.Fields{
		"lvolName":   lvolName,
		"backupUrl":  backupUrl,
		"backupName": backupName,
	})

	executor, err := helperutil.NewExecutor(commonTypes.ProcDirectory)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create executor")
	}

	return &Restore{
		spdkClient:             spdkClient,
		replica:                replica,
		BackupURL:              backupUrl,
		CurrentRestoringBackup: backupName,
		LvolName:               lvolName,
		ip:                     replica.IP,
		port:                   replica.PortStart,
		executor:               executor,
		State:                  btypes.ProgressStateInProgress,
		Progress:               0,
		stopChan:               make(chan struct{}),
		log:                    log,
	}, nil
}

func (r *Restore) StartNewRestore(backupUrl, currentRestoringBackup, lvolName string, validLastRestoredBackup bool) {
	r.Lock()
	defer r.Unlock()

	r.LvolName = lvolName

	r.Progress = 0
	r.Error = ""
	r.BackupURL = backupUrl
	r.State = btypes.ProgressStateInProgress
	if !validLastRestoredBackup {
		r.LastRestored = ""
	}
	r.CurrentRestoringBackup = currentRestoringBackup
}

func (r *Restore) DeepCopy() *Restore {
	r.RLock()
	defer r.RUnlock()

	return &Restore{
		LvolName:               r.LvolName,
		LastRestored:           r.LastRestored,
		BackupURL:              r.BackupURL,
		CurrentRestoringBackup: r.CurrentRestoringBackup,
		State:                  r.State,
		Error:                  r.Error,
		Progress:               r.Progress,
	}
}

func BackupRestore(backupURL, snapshotLvolName string, concurrentLimit int32, restoreObj *Restore) error {
	backupURL = butil.UnescapeURL(backupURL)

	logrus.WithFields(logrus.Fields{
		"backupURL":        backupURL,
		"snapshotLvolName": snapshotLvolName,
		"concurrentLimit":  concurrentLimit,
	}).Info("Start restoring backup")

	return backupstore.RestoreDeltaBlockBackup(&backupstore.DeltaRestoreConfig{
		BackupURL:       backupURL,
		DeltaOps:        restoreObj,
		Filename:        snapshotLvolName,
		ConcurrentLimit: int32(concurrentLimit),
	})
}

func (r *Restore) OpenVolumeDev(volDevName string) (*os.File, string, error) {
	lvolName := r.replica.Name

	r.log.Info("Unexposing lvol bdev before restoration")
	if r.replica.IsExposed {
		err := r.spdkClient.StopExposeBdev(helpertypes.GetNQN(lvolName))
		if err != nil {
			return nil, "", errors.Wrapf(err, "failed to unexpose lvol bdev %v", lvolName)
		}
		r.replica.IsExposed = false
	}

	r.log.Info("Exposing snapshot lvol bdev for restore")
	subsystemNQN, controllerName, err := exposeSnapshotLvolBdev(r.spdkClient, r.replica.LvsName, lvolName, r.ip, r.port, r.executor)
	if err != nil {
		r.log.WithError(err).Errorf("Failed to expose lvol bdev")
		return nil, "", err
	}
	r.subsystemNQN = subsystemNQN
	r.controllerName = controllerName
	r.replica.IsExposed = true
	r.log.Infof("Exposed snapshot lvol bdev %v, subsystemNQN=%v, controllerName %v", lvolName, subsystemNQN, controllerName)

	r.log.Info("Creating NVMe initiator for lvol bdev")
	initiator, err := nvme.NewInitiator(lvolName, helpertypes.GetNQN(lvolName), nvme.HostProc)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to create NVMe initiator for lvol bdev %v", lvolName)
	}
	if err := initiator.Start(r.ip, strconv.Itoa(int(r.port)), false); err != nil {
		return nil, "", errors.Wrapf(err, "failed to start NVMe initiator for lvol bdev %v", lvolName)
	}
	r.initiator = initiator

	r.log.Infof("Opening NVMe device %v", r.initiator.Endpoint)
	fh, err := os.OpenFile(r.initiator.Endpoint, os.O_RDONLY, 0666)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to open NVMe device %v for lvol bdev %v", r.initiator.Endpoint, lvolName)
	}

	return fh, r.initiator.Endpoint, err
}

func (r *Restore) CloseVolumeDev(volDev *os.File) error {
	r.log.Infof("Closing nvme device %v", r.initiator.Endpoint)
	if err := volDev.Close(); err != nil {
		return errors.Wrapf(err, "failed to close nvme device %v", r.initiator.Endpoint)
	}

	r.log.Info("Stopping NVMe initiator")
	if err := r.initiator.Stop(false); err != nil {
		return errors.Wrapf(err, "failed to stop NVMe initiator")
	}

	if !r.replica.IsExposeRequired {
		r.log.Info("Unexposing lvol bdev")
		lvolName := r.replica.Name
		err := r.spdkClient.StopExposeBdev(helpertypes.GetNQN(lvolName))
		if err != nil {
			return errors.Wrapf(err, "failed to unexpose lvol bdev %v", lvolName)
		}
		r.replica.IsExposed = false
	}

	return nil
}

func (r *Restore) UpdateRestoreStatus(snapshotLvolName string, progress int, err error) {
	r.Lock()
	defer r.Unlock()

	r.LvolName = snapshotLvolName
	r.Progress = progress
	if err != nil {
		if r.Error != "" {
			r.Error = fmt.Sprintf("%v: %v", err.Error(), r.Error)
		} else {
			r.Error = err.Error()
		}
		r.State = btypes.ProgressStateError
		r.CurrentRestoringBackup = ""
	}
}

func (r *Restore) FinishRestore() {
	r.Lock()
	defer r.Unlock()

	if r.State != btypes.ProgressStateError {
		r.State = btypes.ProgressStateComplete
		r.LastRestored = r.CurrentRestoringBackup
		r.CurrentRestoringBackup = ""
	}
}

func (r *Restore) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopChan)
	})
}

func (r *Restore) GetStopChan() chan struct{} {
	return r.stopChan
}

// TODL: implement this
// func (status *RestoreStatus) Revert(previousStatus *RestoreStatus) {
// }
