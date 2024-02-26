# -*- coding: utf-8 -*-
# Generated by the protocol buffer compiler.  DO NOT EDIT!
# source: enginerpc/controller.proto
"""Generated protocol buffer code."""
from google.protobuf import descriptor as _descriptor
from google.protobuf import descriptor_pool as _descriptor_pool
from google.protobuf import symbol_database as _symbol_database
from google.protobuf.internal import builder as _builder
# @@protoc_insertion_point(imports)

_sym_db = _symbol_database.Default()


from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2
from enginerpc import common_pb2 as enginerpc_dot_common__pb2


DESCRIPTOR = _descriptor_pool.Default().AddSerializedFile(b'\n\x1a\x65nginerpc/controller.proto\x12\tenginerpc\x1a\x1bgoogle/protobuf/empty.proto\x1a\x16\x65nginerpc/common.proto\"\xa8\x02\n\x06Volume\x12\x0c\n\x04name\x18\x01 \x01(\t\x12\x0c\n\x04size\x18\x02 \x01(\x03\x12\x14\n\x0creplicaCount\x18\x03 \x01(\x05\x12\x10\n\x08\x65ndpoint\x18\x04 \x01(\t\x12\x10\n\x08\x66rontend\x18\x05 \x01(\t\x12\x15\n\rfrontendState\x18\x06 \x01(\t\x12\x13\n\x0bisExpanding\x18\x07 \x01(\x08\x12\x1c\n\x14last_expansion_error\x18\x08 \x01(\t\x12 \n\x18last_expansion_failed_at\x18\t \x01(\t\x12%\n\x1dunmap_mark_snap_chain_removed\x18\n \x01(\x08\x12\x1a\n\x12snapshot_max_count\x18\x0b \x01(\x05\x12\x19\n\x11snapshot_max_size\x18\x0c \x01(\x03\"7\n\x0eReplicaAddress\x12\x0f\n\x07\x61\x64\x64ress\x18\x01 \x01(\t\x12\x14\n\x0cinstanceName\x18\x02 \x01(\t\"e\n\x11\x43ontrollerReplica\x12*\n\x07\x61\x64\x64ress\x18\x01 \x01(\x0b\x32\x19.enginerpc.ReplicaAddress\x12$\n\x04mode\x18\x02 \x01(\x0e\x32\x16.enginerpc.ReplicaMode\"Q\n\x12VolumeStartRequest\x12\x18\n\x10replicaAddresses\x18\x01 \x03(\t\x12\x0c\n\x04size\x18\x02 \x01(\x03\x12\x13\n\x0b\x63urrentSize\x18\x03 \x01(\x03\"\x92\x01\n\x15VolumeSnapshotRequest\x12\x0c\n\x04name\x18\x01 \x01(\t\x12<\n\x06labels\x18\x02 \x03(\x0b\x32,.enginerpc.VolumeSnapshotRequest.LabelsEntry\x1a-\n\x0bLabelsEntry\x12\x0b\n\x03key\x18\x01 \x01(\t\x12\r\n\x05value\x18\x02 \x01(\t:\x02\x38\x01\"#\n\x13VolumeSnapshotReply\x12\x0c\n\x04name\x18\x01 \x01(\t\"#\n\x13VolumeRevertRequest\x12\x0c\n\x04name\x18\x01 \x01(\t\"#\n\x13VolumeExpandRequest\x12\x0c\n\x04size\x18\x01 \x01(\x03\".\n\x1aVolumeFrontendStartRequest\x12\x10\n\x08\x66rontend\x18\x01 \x01(\t\"<\n)VolumeUnmapMarkSnapChainRemovedSetRequest\x12\x0f\n\x07\x65nabled\x18\x01 \x01(\x08\"1\n VolumeSnapshotMaxCountSetRequest\x12\r\n\x05\x63ount\x18\x01 \x01(\x05\"/\n\x1fVolumeSnapshotMaxSizeSetRequest\x12\x0c\n\x04size\x18\x01 \x01(\x03\"3\n\x1bVolumePrepareRestoreRequest\x12\x14\n\x0clastRestored\x18\x01 \x01(\t\"5\n\x1aVolumeFinishRestoreRequest\x12\x17\n\x0f\x63urrentRestored\x18\x01 \x01(\t\"B\n\x10ReplicaListReply\x12.\n\x08replicas\x18\x01 \x03(\x0b\x32\x1c.enginerpc.ControllerReplica\"r\n\x1e\x43ontrollerReplicaCreateRequest\x12\x0f\n\x07\x61\x64\x64ress\x18\x01 \x01(\t\x12\x19\n\x11snapshot_required\x18\x02 \x01(\x08\x12$\n\x04mode\x18\x03 \x01(\x0e\x32\x16.enginerpc.ReplicaMode\"\x81\x01\n\x1aReplicaPrepareRebuildReply\x12-\n\x07replica\x18\x01 \x01(\x0b\x32\x1c.enginerpc.ControllerReplica\x12\x34\n\x13sync_file_info_list\x18\x02 \x03(\x0b\x32\x17.enginerpc.SyncFileInfo\"#\n\x12JournalListRequest\x12\r\n\x05limit\x18\x01 \x01(\x03\"\xef\x01\n\rVersionOutput\x12\x0f\n\x07version\x18\x01 \x01(\t\x12\x11\n\tgitCommit\x18\x02 \x01(\t\x12\x11\n\tbuildDate\x18\x03 \x01(\t\x12\x15\n\rcliAPIVersion\x18\x04 \x01(\x03\x12\x18\n\x10\x63liAPIMinVersion\x18\x05 \x01(\x03\x12\x1c\n\x14\x63ontrollerAPIVersion\x18\x06 \x01(\x03\x12\x1f\n\x17\x63ontrollerAPIMinVersion\x18\x07 \x01(\x03\x12\x19\n\x11\x64\x61taFormatVersion\x18\x08 \x01(\x03\x12\x1c\n\x14\x64\x61taFormatMinVersion\x18\t \x01(\x03\"B\n\x15VersionDetailGetReply\x12)\n\x07version\x18\x01 \x01(\x0b\x32\x18.enginerpc.VersionOutput\"\x8a\x01\n\x07Metrics\x12\x16\n\x0ereadThroughput\x18\x01 \x01(\x04\x12\x17\n\x0fwriteThroughput\x18\x02 \x01(\x04\x12\x13\n\x0breadLatency\x18\x03 \x01(\x04\x12\x14\n\x0cwriteLatency\x18\x04 \x01(\x04\x12\x10\n\x08readIOPS\x18\x05 \x01(\x04\x12\x11\n\twriteIOPS\x18\x06 \x01(\x04\"6\n\x0fMetricsGetReply\x12#\n\x07metrics\x18\x01 \x01(\x0b\x32\x12.enginerpc.Metrics*&\n\x0bReplicaMode\x12\x06\n\x02WO\x10\x00\x12\x06\n\x02RW\x10\x01\x12\x07\n\x03\x45RR\x10\x02\x32\xe2\x0c\n\x11\x43ontrollerService\x12\x36\n\tVolumeGet\x12\x16.google.protobuf.Empty\x1a\x11.enginerpc.Volume\x12?\n\x0bVolumeStart\x12\x1d.enginerpc.VolumeStartRequest\x1a\x11.enginerpc.Volume\x12;\n\x0eVolumeShutdown\x12\x16.google.protobuf.Empty\x1a\x11.enginerpc.Volume\x12R\n\x0eVolumeSnapshot\x12 .enginerpc.VolumeSnapshotRequest\x1a\x1e.enginerpc.VolumeSnapshotReply\x12\x41\n\x0cVolumeRevert\x12\x1e.enginerpc.VolumeRevertRequest\x1a\x11.enginerpc.Volume\x12\x41\n\x0cVolumeExpand\x12\x1e.enginerpc.VolumeExpandRequest\x1a\x11.enginerpc.Volume\x12O\n\x13VolumeFrontendStart\x12%.enginerpc.VolumeFrontendStartRequest\x1a\x11.enginerpc.Volume\x12\x43\n\x16VolumeFrontendShutdown\x12\x16.google.protobuf.Empty\x1a\x11.enginerpc.Volume\x12m\n\"VolumeUnmapMarkSnapChainRemovedSet\x12\x34.enginerpc.VolumeUnmapMarkSnapChainRemovedSetRequest\x1a\x11.enginerpc.Volume\x12[\n\x19VolumeSnapshotMaxCountSet\x12+.enginerpc.VolumeSnapshotMaxCountSetRequest\x1a\x11.enginerpc.Volume\x12Y\n\x18VolumeSnapshotMaxSizeSet\x12*.enginerpc.VolumeSnapshotMaxSizeSetRequest\x1a\x11.enginerpc.Volume\x12\x42\n\x0bReplicaList\x12\x16.google.protobuf.Empty\x1a\x1b.enginerpc.ReplicaListReply\x12\x45\n\nReplicaGet\x12\x19.enginerpc.ReplicaAddress\x1a\x1c.enginerpc.ControllerReplica\x12\x62\n\x17\x43ontrollerReplicaCreate\x12).enginerpc.ControllerReplicaCreateRequest\x1a\x1c.enginerpc.ControllerReplica\x12\x42\n\rReplicaDelete\x12\x19.enginerpc.ReplicaAddress\x1a\x16.google.protobuf.Empty\x12K\n\rReplicaUpdate\x12\x1c.enginerpc.ControllerReplica\x1a\x1c.enginerpc.ControllerReplica\x12Y\n\x15ReplicaPrepareRebuild\x12\x19.enginerpc.ReplicaAddress\x1a%.enginerpc.ReplicaPrepareRebuildReply\x12O\n\x14ReplicaVerifyRebuild\x12\x19.enginerpc.ReplicaAddress\x1a\x1c.enginerpc.ControllerReplica\x12\x44\n\x0bJournalList\x12\x1d.enginerpc.JournalListRequest\x1a\x16.google.protobuf.Empty\x12L\n\x10VersionDetailGet\x12\x16.google.protobuf.Empty\x1a .enginerpc.VersionDetailGetReply\x12@\n\nMetricsGet\x12\x16.google.protobuf.Empty\x1a\x1a.enginerpc.MetricsGetReplyB)Z\'github.com/longhorn/types/pkg/enginerpcb\x06proto3')

_globals = globals()
_builder.BuildMessageAndEnumDescriptors(DESCRIPTOR, _globals)
_builder.BuildTopDescriptorsAndMessages(DESCRIPTOR, 'enginerpc.controller_pb2', _globals)
if _descriptor._USE_C_DESCRIPTORS == False:

  DESCRIPTOR._options = None
  DESCRIPTOR._serialized_options = b'Z\'github.com/longhorn/types/pkg/enginerpc'
  _VOLUMESNAPSHOTREQUEST_LABELSENTRY._options = None
  _VOLUMESNAPSHOTREQUEST_LABELSENTRY._serialized_options = b'8\001'
  _globals['_REPLICAMODE']._serialized_start=2074
  _globals['_REPLICAMODE']._serialized_end=2112
  _globals['_VOLUME']._serialized_start=95
  _globals['_VOLUME']._serialized_end=391
  _globals['_REPLICAADDRESS']._serialized_start=393
  _globals['_REPLICAADDRESS']._serialized_end=448
  _globals['_CONTROLLERREPLICA']._serialized_start=450
  _globals['_CONTROLLERREPLICA']._serialized_end=551
  _globals['_VOLUMESTARTREQUEST']._serialized_start=553
  _globals['_VOLUMESTARTREQUEST']._serialized_end=634
  _globals['_VOLUMESNAPSHOTREQUEST']._serialized_start=637
  _globals['_VOLUMESNAPSHOTREQUEST']._serialized_end=783
  _globals['_VOLUMESNAPSHOTREQUEST_LABELSENTRY']._serialized_start=738
  _globals['_VOLUMESNAPSHOTREQUEST_LABELSENTRY']._serialized_end=783
  _globals['_VOLUMESNAPSHOTREPLY']._serialized_start=785
  _globals['_VOLUMESNAPSHOTREPLY']._serialized_end=820
  _globals['_VOLUMEREVERTREQUEST']._serialized_start=822
  _globals['_VOLUMEREVERTREQUEST']._serialized_end=857
  _globals['_VOLUMEEXPANDREQUEST']._serialized_start=859
  _globals['_VOLUMEEXPANDREQUEST']._serialized_end=894
  _globals['_VOLUMEFRONTENDSTARTREQUEST']._serialized_start=896
  _globals['_VOLUMEFRONTENDSTARTREQUEST']._serialized_end=942
  _globals['_VOLUMEUNMAPMARKSNAPCHAINREMOVEDSETREQUEST']._serialized_start=944
  _globals['_VOLUMEUNMAPMARKSNAPCHAINREMOVEDSETREQUEST']._serialized_end=1004
  _globals['_VOLUMESNAPSHOTMAXCOUNTSETREQUEST']._serialized_start=1006
  _globals['_VOLUMESNAPSHOTMAXCOUNTSETREQUEST']._serialized_end=1055
  _globals['_VOLUMESNAPSHOTMAXSIZESETREQUEST']._serialized_start=1057
  _globals['_VOLUMESNAPSHOTMAXSIZESETREQUEST']._serialized_end=1104
  _globals['_VOLUMEPREPARERESTOREREQUEST']._serialized_start=1106
  _globals['_VOLUMEPREPARERESTOREREQUEST']._serialized_end=1157
  _globals['_VOLUMEFINISHRESTOREREQUEST']._serialized_start=1159
  _globals['_VOLUMEFINISHRESTOREREQUEST']._serialized_end=1212
  _globals['_REPLICALISTREPLY']._serialized_start=1214
  _globals['_REPLICALISTREPLY']._serialized_end=1280
  _globals['_CONTROLLERREPLICACREATEREQUEST']._serialized_start=1282
  _globals['_CONTROLLERREPLICACREATEREQUEST']._serialized_end=1396
  _globals['_REPLICAPREPAREREBUILDREPLY']._serialized_start=1399
  _globals['_REPLICAPREPAREREBUILDREPLY']._serialized_end=1528
  _globals['_JOURNALLISTREQUEST']._serialized_start=1530
  _globals['_JOURNALLISTREQUEST']._serialized_end=1565
  _globals['_VERSIONOUTPUT']._serialized_start=1568
  _globals['_VERSIONOUTPUT']._serialized_end=1807
  _globals['_VERSIONDETAILGETREPLY']._serialized_start=1809
  _globals['_VERSIONDETAILGETREPLY']._serialized_end=1875
  _globals['_METRICS']._serialized_start=1878
  _globals['_METRICS']._serialized_end=2016
  _globals['_METRICSGETREPLY']._serialized_start=2018
  _globals['_METRICSGETREPLY']._serialized_end=2072
  _globals['_CONTROLLERSERVICE']._serialized_start=2115
  _globals['_CONTROLLERSERVICE']._serialized_end=3749
# @@protoc_insertion_point(module_scope)
