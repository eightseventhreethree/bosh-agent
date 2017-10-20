package disk

import "fmt"

type PartitionType string

const (
	PartitionTypeSwap    PartitionType = "swap"
	PartitionTypeLinux   PartitionType = "linux"
	PartitionTypeEmpty   PartitionType = "empty"
	PartitionTypeUnknown PartitionType = "unknown"
)

type Partition struct {
	SizeInBytes uint64
	Type        PartitionType
	SectorInfo  *PartitionSectorInfo
}

type PartitionSectorInfo struct {
	Start         uint64
	SizeInSectors uint64
}

type Partitioner interface {
	Partition(devicePath string, partitions []Partition) (err error)
	GetDeviceSizeInBytes(devicePath string) (size uint64, err error)
}

func (p Partition) String() string {
	return fmt.Sprintf("[Type: %s, SizeInBytes: %d]", p.Type, p.SizeInBytes)
}
