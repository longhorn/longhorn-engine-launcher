package spdk

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/longhorn/go-spdk-helper/pkg/jsonrpc"
	spdktypes "github.com/longhorn/go-spdk-helper/pkg/spdk/types"
	spdkutil "github.com/longhorn/go-spdk-helper/pkg/util"

	"github.com/longhorn/longhorn-spdk-engine/pkg/util"
	"github.com/longhorn/longhorn-spdk-engine/proto/spdkrpc"
)

const (
	defaultClusterSize = 1 * 1024 * 1024 // 1MB
	defaultBlockSize   = 4096            // 4KB

	hostPrefix = "/host"
)

func svcDiskCreate(spdkClient *SPDKClient, diskName, diskPath string, blockSize int64) (ret *spdkrpc.Disk, err error) {
	log := logrus.WithFields(logrus.Fields{
		"diskName":  diskName,
		"diskPath":  diskPath,
		"blockSize": blockSize,
	})

	log.Info("Creating disk")
	defer func() {
		if err != nil {
			log.WithError(err).Error("Failed to create disk")
		} else {
			log.Info("Created disk")
		}
	}()

	if diskName == "" || diskPath == "" {
		return nil, grpcstatus.Error(grpccodes.InvalidArgument, "disk name and disk path are required")
	}

	if err := validateDiskCreation(spdkClient, diskPath); err != nil {
		return nil, grpcstatus.Error(grpccodes.InvalidArgument, errors.Wrap(err, "failed to validate disk create request").Error())
	}

	if blockSize <= 0 {
		blockSize = int64(defaultBlockSize)
		log.Infof("Using default block size %v", blockSize)
	}

	uuid, err := addBlockDevice(spdkClient, diskName, diskPath, blockSize)
	if err != nil {
		return nil, grpcstatus.Error(grpccodes.Internal, errors.Wrap(err, "failed to add block device").Error())
	}

	return lvstoreToDisk(spdkClient, diskPath, "", uuid)
}

func svcDiskDelete(spdkClient *SPDKClient, diskName, diskUUID string) (ret *emptypb.Empty, err error) {
	log := logrus.WithFields(logrus.Fields{
		"diskName": diskName,
		"diskUUID": diskUUID,
	})

	log.Info("Deleting disk")
	defer func() {
		if err != nil {
			log.WithError(err).Error("Failed to delete disk")
		} else {
			log.Info("Deleted disk")
		}
	}()

	if diskName == "" || diskUUID == "" {
		return &empty.Empty{}, grpcstatus.Error(grpccodes.InvalidArgument, "disk name and disk UUID are required")
	}

	aioBdevName := ""
	lvstores, err := spdkClient.BdevLvolGetLvstore("", diskUUID)
	if err != nil {
		if !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return nil, errors.Wrapf(err, "failed to get lvstore with UUID %v", diskUUID)
		}
		log.Warnf("Cannot find lvstore with UUID %v", diskUUID)
		aioBdevName = diskName
	} else {
		lvstore := &lvstores[0]
		if lvstore.Name != diskName {
			log.Warnf("Disk name %v does not match lvstore name %v", diskName, lvstore.Name)
			return nil, grpcstatus.Errorf(grpccodes.NotFound, "disk name %v does not match lvstore name %v", diskName, lvstore.Name)
		}
		aioBdevName = lvstore.BaseBdev
	}

	if _, err := spdkClient.BdevAioDelete(aioBdevName); err != nil {
		return nil, errors.Wrapf(err, "failed to delete AIO bdev %v", aioBdevName)
	}
	return &empty.Empty{}, nil
}

func svcDiskGet(spdkClient *SPDKClient, diskName string) (ret *spdkrpc.Disk, err error) {
	log := logrus.WithFields(logrus.Fields{
		"diskName": diskName,
	})

	log.Info("Getting disk info")
	defer func() {
		if err != nil {
			log.WithError(err).Error("Failed to get disk info")
		} else {
			log.Info("Got disk info")
		}
	}()

	if diskName == "" {
		return nil, grpcstatus.Error(grpccodes.InvalidArgument, "disk name is required")
	}

	// Check if the disk exists
	bdevs, err := spdkClient.BdevAioGet(diskName, 0)
	if err != nil {
		if !jsonrpc.IsJSONRPCRespErrorNoSuchDevice(err) {
			return nil, grpcstatus.Errorf(grpccodes.Internal, errors.Wrapf(err, "failed to get AIO bdev with name %v", diskName).Error())
		}
	}
	if len(bdevs) == 0 {
		return nil, grpcstatus.Errorf(grpccodes.NotFound, "cannot find AIO bdev with name %v", diskName)
	}

	var targetBdev *spdktypes.BdevInfo
	for i, bdev := range bdevs {
		if bdev.DriverSpecific != nil {
			targetBdev = &bdevs[i]
			break
		}
	}
	if targetBdev == nil {
		return nil, grpcstatus.Errorf(grpccodes.NotFound, errors.Wrapf(err, "failed to get AIO bdev for disk %v", diskName).Error())
	}

	diskPath := util.RemovePrefix(targetBdev.DriverSpecific.Aio.FileName, hostPrefix)

	return lvstoreToDisk(spdkClient, diskPath, diskName, "")
}

func getDiskPath(path string) string {
	return filepath.Join(hostPrefix, path)
}

func getDiskID(filename string) (string, error) {
	executor := spdkutil.NewTimeoutExecutor(spdkutil.CmdTimeout)
	dev, err := spdkutil.DetectDevice(filename, executor)
	if err != nil {
		return "", errors.Wrap(err, "failed to detect disk device")
	}

	return fmt.Sprintf("%d-%d", dev.Major, dev.Minor), nil
}

func isBlockDevice(fullPath string) (bool, error) {
	var st unix.Stat_t
	err := unix.Stat(fullPath, &st)
	if err != nil {
		return false, err
	}

	return (st.Mode & unix.S_IFMT) == unix.S_IFBLK, nil
}

func getDiskDeviceSize(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to open %s", path)
	}
	defer file.Close()

	pos, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to seek %s", path)
	}
	return pos, nil
}

func validateDiskCreation(spdkClient *SPDKClient, diskPath string) error {
	ok, err := isBlockDevice(diskPath)
	if err != nil {
		return errors.Wrap(err, "failed to check if disk is a block device")
	}
	if !ok {
		return errors.Wrapf(err, "disk %v is not a block device", diskPath)
	}

	size, err := getDiskDeviceSize(diskPath)
	if err != nil {
		return errors.Wrap(err, "failed to get disk size")
	}
	if size == 0 {
		return fmt.Errorf("disk %v size is 0", diskPath)
	}

	diskID, err := getDiskID(getDiskPath(diskPath))
	if err != nil {
		return errors.Wrap(err, "failed to get disk device number")
	}

	bdevs, err := spdkClient.BdevAioGet("", 0)
	if err != nil {
		return errors.Wrap(err, "failed to get AIO bdevs")
	}

	for _, bdev := range bdevs {
		id, err := getDiskID(bdev.DriverSpecific.Aio.FileName)
		if err != nil {
			return errors.Wrap(err, "failed to get disk device number")
		}

		if id == diskID {
			return fmt.Errorf("disk %v is already used by AIO bdev %v", diskPath, bdev.Name)
		}
	}

	return nil
}

func addBlockDevice(spdkClient *SPDKClient, diskName, diskPath string, blockSize int64) (string, error) {
	log := logrus.WithFields(logrus.Fields{
		"diskName":  diskName,
		"diskPath":  diskPath,
		"blockSize": blockSize,
	})

	log.Infof("Creating AIO bdev %v with block size %v", diskName, blockSize)
	bdevName, err := spdkClient.BdevAioCreate(getDiskPath(diskPath), diskName, uint64(blockSize))
	if err != nil {
		if !jsonrpc.IsJSONRPCRespErrorFileExists(err) {
			return "", errors.Wrapf(err, "failed to create AIO bdev")
		}
	}

	log.Infof("Creating lvstore %v", diskName)

	// Name of the lvstore is the same as the name of the aio bdev
	lvstoreName := bdevName
	lvstores, err := spdkClient.BdevLvolGetLvstore("", "")
	if err != nil {
		return "", errors.Wrapf(err, "failed to get lvstores")
	}

	for _, lvstore := range lvstores {
		if lvstore.BaseBdev != bdevName {
			continue
		}

		log.Infof("Found an existing lvstore %v", lvstore.Name)
		if lvstore.Name == lvstoreName {
			return lvstore.UUID, nil
		}

		log.Infof("Renaming the existing lvstore %v to %v", lvstore.Name, lvstoreName)
		renamed, err := spdkClient.BdevLvolRenameLvstore(lvstore.Name, lvstoreName)
		if err != nil {
			return "", errors.Wrapf(err, "failed to rename lvstore from %v to %v", lvstore.Name, lvstoreName)
		}
		if !renamed {
			return "", fmt.Errorf("failed to rename lvstore from %v to %v", lvstore.Name, lvstoreName)
		}
		return lvstore.UUID, nil
	}

	return spdkClient.BdevLvolCreateLvstore(lvstoreName, diskName, defaultClusterSize)
}

func lvstoreToDisk(spdkClient *SPDKClient, diskPath, lvstoreName, lvstoreUUID string) (*spdkrpc.Disk, error) {
	lvstores, err := spdkClient.BdevLvolGetLvstore(lvstoreName, lvstoreUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get lvstore with name %v and UUID %v", lvstoreName, lvstoreUUID)
	}
	lvstore := &lvstores[0]

	// A disk does not have a fsid, so we use the device number as the disk ID
	diskID, err := getDiskID(getDiskPath(diskPath))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get disk ID")
	}

	return &spdkrpc.Disk{
		Id:          diskID,
		Uuid:        lvstore.UUID,
		Path:        diskPath,
		Type:        DiskTypeBlock,
		TotalSize:   int64(lvstore.TotalDataClusters * lvstore.ClusterSize),
		FreeSize:    int64(lvstore.FreeClusters * lvstore.ClusterSize),
		TotalBlocks: int64(lvstore.TotalDataClusters * lvstore.ClusterSize / lvstore.BlockSize),
		FreeBlocks:  int64(lvstore.FreeClusters * lvstore.ClusterSize / lvstore.BlockSize),
		BlockSize:   int64(lvstore.BlockSize),
		ClusterSize: int64(lvstore.ClusterSize),
	}, nil
}
