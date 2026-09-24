package jobqueue

import (
	"syscall"
	"unsafe"
)

type memoryStatus struct {
	Length               uint32
	Load                 uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var globalMemoryStatus = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func AvailableMemory() uint64 {
	state := memoryStatus{Length: uint32(unsafe.Sizeof(memoryStatus{}))}
	ok, _, _ := globalMemoryStatus.Call(uintptr(unsafe.Pointer(&state)))
	if ok == 0 {
		return 0
	}
	return state.AvailPhys
}
