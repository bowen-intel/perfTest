//go:build libpfm && cgo
// +build libpfm,cgo

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

// #cgo CFLAGS: -I/usr/include
// #cgo LDFLAGS: -lpfm
// #include <stdlib.h>
// #include <string.h>
import "C"

func init() {
	cs := C.CString("cycles")
	defer C.free(unsafe.Pointer(cs))
}

// #include<stdlib.h>
type groupReadFormat struct {
	Nr          uint64
	TimeEnabled uint64
	TimeRunning uint64
}

type perfvalue struct {
	Value uint64
	ID    uint64
}

func main() {
	fp, err := os.Open("/sys/fs/cgroup/kubepods.slice/kubepods-pod4cf35eac_fc10_4600_b419_2fa240cf24b9.slice/cri-containerd-e3e4e7a14e0c6b23d2eb5f9c603dd7ccd505de4241faf4f46e390e64ccdf8548.scope")
	if err != nil {
		klog.Fatal(err)
	}
	defer fp.Close()
	m := make(map[string]float64)
	for i := 0; i < 1000; i++ {
		for j := 0; j < runtime.NumCPU(); j++ {
			attr1 := &unix.PerfEventAttr{}
			attr1.Type = 0 // hw
			attr1.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
			attr1.Config = 0 // cycles

			attr1.Sample_type = unix.PERF_SAMPLE_IDENTIFIER
			attr1.Read_format = unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_GROUP | unix.PERF_FORMAT_ID
			attr1.Bits = unix.PerfBitDisabled | unix.PerfBitInherit

			defaultFd := -1
			fd1, _, err := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN, uintptr(unsafe.Pointer(attr1)),
				fp.Fd(), uintptr(j), uintptr(defaultFd), uintptr(unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC), uintptr(0))
			// fd1, err := unix.PerfEventOpen(attr1, int(fp.Fd()), j, -1, unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC)
			if err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			var id1, id2 uint64
			id2event := make(map[uint64]string)
			_, _, err = syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd1), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id1)), 0, 0, 0)
			if err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			id2event[id1] = "cycles"
			attr2 := &unix.PerfEventAttr{}
			attr2.Type = 0
			attr2.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
			attr2.Config = 1
			attr2.Sample_type = unix.PERF_SAMPLE_IDENTIFIER
			attr2.Bits = unix.PerfBitInherit
			attr2.Read_format = unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_GROUP | unix.PERF_FORMAT_ID
			fd2, _, err := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN, uintptr(unsafe.Pointer(attr2)), fp.Fd(), uintptr(j), fd1, uintptr(unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC), uintptr(0))
			// fd2, err := unix.PerfEventOpen(attr2, int(fp.Fd()), j, fd1, unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC)
			if err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd2), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id2)))
			if err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			id2event[id2] = "instructions"
			if _, _, err = syscall.Syscall6(syscall.SYS_IOCTL, fd1, unix.PERF_EVENT_IOC_RESET, unix.PERF_IOC_FLAG_GROUP, 0, 0, 0); err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			if _, _, err = syscall.Syscall6(syscall.SYS_IOCTL, fd1, unix.PERF_EVENT_IOC_ENABLE, unix.PERF_IOC_FLAG_GROUP, 0, 0, 0); err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			time.Sleep(time.Nanosecond)
			if _, _, err = syscall.Syscall6(syscall.SYS_IOCTL, fd1, unix.PERF_EVENT_IOC_DISABLE, unix.PERF_IOC_FLAG_GROUP, 0, 0, 0); err != syscall.Errno(0) {
				klog.Fatal(err)
			}
			buf := make([]byte, 3*8+16*2)
			file := os.NewFile(uintptr(fd1), "cycles")
			file.Read(buf)
			file.Close()
			file2 := os.NewFile(uintptr(fd2), "instructions")
			file2.Close()
			reader := bytes.NewReader(buf)
			header := &groupReadFormat{}
			if rerr := binary.Read(reader, binary.LittleEndian, header); rerr != nil {
				klog.Fatal(err)
			}

			for i := 0; i < int(header.Nr); i++ {
				value := &perfvalue{}
				if rerr := binary.Read(reader, binary.LittleEndian, value); rerr != nil {
					klog.Fatal(err)
				}
				event := id2event[value.ID]
				m[event] = float64(value.Value)
			}
		}
	}
}
