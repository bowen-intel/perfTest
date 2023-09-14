//go:build libpfm && cgo
// +build libpfm,cgo

package rawcollector

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Rouzip/goperf/pkg/metrics"
	"github.com/Rouzip/goperf/pkg/utils"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// #cgo CFLAGS: -I/usr/include -I/usr/local/include
// #cgo LDFLAGS: -lpfm
// #include <perfmon/pfmlib.h>
// #include <stdlib.h>
// #include <string.h>
import "C"

var (
	initLibpfm     sync.Once
	finalizeLibpfm sync.Once
	bufPool        sync.Pool
	eventAttr      map[string]*unix.PerfEventAttr
)

type groupReadFormat struct {
	Nr          uint64
	TimeEnabled uint64
	TimeRunning uint64
}

func init() {
	initLibpfm.Do(func() {
		if err := C.pfm_initialize(); err != C.PFM_SUCCESS {
			panic("failed to initialize libpfm")
		}
	})
	bufPool = sync.Pool{
		New: func() interface{} {
			p := make([]byte, 24+16*2)
			return &p
		},
	}
	eventAttr = make(map[string]*unix.PerfEventAttr)
}

func Finalize() {
	finalizeLibpfm.Do(func() {
		C.pfm_terminate()
	})
}

type EventsGroup struct {
	EventsGroup []Group
}

type Group struct {
	Events []string
}

type RawCollector struct {
	CGroupFd     *os.File
	CPUCollector map[int]group // groups of events
	// assume that all events are unique
	Values     map[string]float64
	EventIDMap map[uint64]string
	Pod        *v1.Pod
	Container  *v1.ContainerStatus
	valCh      chan perfValue
	idCh       chan perfId
}

type perfValue struct {
	Value uint64
	ID    uint64
}

type perfId struct {
	id    uint64
	event string
}

type group struct {
	leaderName string
	eventNames []string
	fds        map[int]io.ReadCloser
	otherFds   []io.ReadCloser
	mu         *sync.Mutex
}

func (g *group) createEnabledFds(cgroupFd *os.File, idMap chan perfId) error {
	eventConfigMap := make(map[string]*unix.PerfEventAttr)
	fdMap := make(map[int]io.ReadCloser)
	for i, event := range g.eventNames {
		config, err := createPerfConfig(event)
		if i == 0 {
			config.Bits = unix.PerfBitDisabled | unix.PerfBitInherit
		} else {
			config.Bits = unix.PerfBitInherit
		}
		config.Sample_type = unix.PERF_SAMPLE_IDENTIFIER
		config.Read_format = unix.PERF_FORMAT_GROUP | unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_ID
		config.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
		// TODO: change location of free?
		defer C.free(unsafe.Pointer(config))
		if err != nil {
			return err
		}
		eventConfigMap[event] = config
	}

	for i := 0; i < utils.CPUNUM; i++ {
		var leaderFd int
		var err error
		for _, event := range g.eventNames {
			if event == g.leaderName {
				attr := eventConfigMap[event]
				defaultLeaderFd := -1
				leaderFdUptr, _, e1 := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN, uintptr(unsafe.Pointer(attr)), uintptr(cgroupFd.Fd()), uintptr(i), uintptr(defaultLeaderFd), uintptr(unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC), 0)
				if e1 != syscall.Errno(0) {
					klog.Error("failed to create perf fd")
					return err
				}
				leaderFd = int(leaderFdUptr)
				perfFd := os.NewFile(uintptr(leaderFd), g.leaderName)
				fdMap[i] = perfFd
				if perfFd == nil {
					return fmt.Errorf("failed to create perfFd")
				}
				g.fds[i] = perfFd
				var id uint64
				_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(leaderFd), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id)))
				if err != 0 {
					klog.Error("failed to use ioctl syscall", err)
					return err
				}
				idMap <- perfId{
					id:    id,
					event: event,
				}
			} else {
				// go func(event string, i int) {
				attr := eventConfigMap[event]
				fd, _, e1 := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN, uintptr(unsafe.Pointer(attr)), uintptr(cgroupFd.Fd()), uintptr(i), uintptr(leaderFd), uintptr(unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC), 0)
				// klog.Info(fd, e1)
				// errNo := syscall.Errno(e1)
				// fd, errNo := unix.PerfEventOpen(attr, int(cgroupFd.Fd()), i, leaderFd, unix.PERF_FLAG_PID_CGROUP|unix.PERF_FLAG_FD_CLOEXEC)
				if e1 != syscall.Errno(0) {
					klog.Error(e1)
				}
				var id uint64
				f := os.NewFile(uintptr(fd), event)
				_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(f.Fd()), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id)))
				if err != syscall.Errno(0) {
					klog.Error(err)
				}
				g.mu.Lock()
				g.otherFds = append(g.otherFds, os.NewFile(uintptr(fd), fmt.Sprintf("%s_%d", event, i)))
				g.mu.Unlock()
				idMap <- perfId{
					id:    id,
					event: event,
				}
				// }(event, i)
			}
		}
	}
	for _, fd := range fdMap {
		if err := unix.IoctlSetInt(int(fd.(*os.File).Fd()), unix.PERF_EVENT_IOC_RESET, 1); err != nil {
			return err
		}
		if err := unix.IoctlSetInt(int(fd.(*os.File).Fd()), unix.PERF_EVENT_IOC_ENABLE, 1); err != nil {
			return err
		}
	}
	return nil
}

func (g *group) collect(ch chan perfValue) error {
	var wg sync.WaitGroup
	wg.Add(len(g.fds))
	for _, fd := range g.fds {
		go func(fd io.ReadCloser) {
			defer wg.Done()
			buf := bufPool.Get().(*[]byte)
			defer bufPool.Put(buf)
			_, err := fd.Read(*buf)
			if err != nil {
				klog.Error(err)
				return
			}

			header := &groupReadFormat{}
			reader := bytes.NewReader(*buf)
			err = binary.Read(reader, binary.LittleEndian, header)
			if err != nil {
				klog.Error(err)
				return
			}
			scalingRatio := 1.0
			if header.TimeRunning != 0 && header.TimeEnabled != 0 {
				scalingRatio = float64(header.TimeRunning) / float64(header.TimeEnabled)
			}
			if scalingRatio != 0 {
				for i := 0; i < int(header.Nr); i++ {
					value := &perfValue{}
					err = binary.Read(reader, binary.LittleEndian, value)
					if err != nil {
						klog.Error(err)
						return
					}
					value.Value = uint64(float64(value.Value) / scalingRatio)
					ch <- *value
				}
			}
		}(fd)
	}
	wg.Wait()
	return nil
}

// just collect Cycles and Instructions for now one group? TODO: just collect core events
func NewRawCollector(pod *v1.Pod, container *v1.ContainerStatus, events EventsGroup) *RawCollector {
	rc := &RawCollector{
		CPUCollector: make(map[int]group),
	}
	fd, err := utils.CGroupFd(pod, container)
	if err != nil {
		klog.Fatal(err)
	}
	rc.CGroupFd = fd
	rc.Values = make(map[string]float64)
	rc.EventIDMap = make(map[uint64]string)
	rc.idCh = make(chan perfId)
	rc.Container = container
	rc.Pod = pod
	go func() {
		// FIXME: how to close this channel?
		// ctx?
		for idMap := range rc.idCh {
			rc.EventIDMap[idMap.id] = idMap.event
		}
	}()
	rc.valCh = make(chan perfValue)
	go func() {
		// FIXME: how to close this channel?
		for val := range rc.valCh {
			rc.Values[rc.EventIDMap[val.ID]] += float64(val.Value)
		}
	}()

	// for poc, change events to group
	perfGroup := &group{
		leaderName: "instructions",
		eventNames: []string{"instructions", "cycles"},
		fds:        make(map[int]io.ReadCloser),
		otherFds:   make([]io.ReadCloser, 1),
		mu:         &sync.Mutex{},
	}
	err = perfGroup.createEnabledFds(rc.CGroupFd, rc.idCh)
	if err != nil {
		klog.Fatal(err)
	}
	rc.CPUCollector[0] = *perfGroup

	// for poc, create a group for test, leader: instructions, instructions, cycles
	return rc
}

func (r *RawCollector) Collect() error {
	var wg sync.WaitGroup
	wg.Add(len(r.CPUCollector))
	for _, gc := range r.CPUCollector {
		// TODO: error fix
		go func(gc group) {
			defer wg.Done()
			gc.collect(r.valCh)
		}(gc)
	}
	wg.Wait()
	for _, gc := range r.CPUCollector {
		for _, fd := range gc.fds {
			fd.Close()
		}
	}
	return nil
}

func (r *RawCollector) Close() error {
	return r.CGroupFd.Close()
}

// caller must free the memory
func createPerfConfig(event string) (*unix.PerfEventAttr, error) {
	// https://pkg.go.dev/cmd/cgo OOM instread of check malloc error
	perfEventAttrPtr := C.malloc(C.ulong(unsafe.Sizeof(unix.PerfEventAttr{})))
	C.memset(perfEventAttrPtr, 0, C.ulong(unsafe.Sizeof(unix.PerfEventAttr{})))
	if err := pfmGetOsEventEncoding(event, perfEventAttrPtr); err != nil {
		return nil, err
	}

	return (*unix.PerfEventAttr)(perfEventAttrPtr), nil
}

// https://man7.org/linux/man-pages/man3/pfm_get_os_event_encoding.3.html
func pfmGetOsEventEncoding(event string, perfEventAttrPtr unsafe.Pointer) error {
	arg := pfmPerfEncodeArgT{}
	arg.attr = perfEventAttrPtr
	fstr := C.CString("")
	defer C.free(unsafe.Pointer(fstr))
	arg.size = C.ulong(unsafe.Sizeof(arg))
	eventCStr := C.CString(event)
	defer C.free(unsafe.Pointer(eventCStr))
	if err := C.pfm_get_os_event_encoding(eventCStr, C.PFM_PLM0|C.PFM_PLM3, C.PFM_OS_PERF_EVENT, unsafe.Pointer(&arg)); err != C.PFM_SUCCESS {
		return fmt.Errorf("failed to get event encoding: %d", err)
	}
	return nil
}

// 1000 * MEM_LOAD_RETIRED.L3_MISS_PS / INST_RETIRED.ANY
type PerfCoreCollector struct {
	Core     int
	leaderFd io.ReadCloser
	otherFds []io.ReadCloser
	id2event map[uint64]string
}

func InitCoreEvent(events []string) {
	for _, event := range events {
		attr, err := createPerfConfig(event)
		if err != nil {
			klog.Fatal(err)
		}
		eventAttr[event] = attr
	}
}

func InitMPKIAttr() {
	l3MissAttr := eventAttr["MEM_LOAD_RETIRED.L3_MISS"]
	l3MissAttr.Bits |= unix.PerfBitDisabled
	l3MissAttr.Read_format = unix.PERF_FORMAT_GROUP | unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_ID
	l3MissAttr.Sample_type = unix.PERF_SAMPLE_IDENTIFIER
	l3MissAttr.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
	l3MissAttr.Bits |= unix.PerfBitInherit
	eventAttr["MEM_LOAD_RETIRED.L3_MISS"] = l3MissAttr
	insAttr := eventAttr["INST_RETIRED.ANY"]
	insAttr.Bits |= unix.PerfBitInherit
	insAttr.Read_format = unix.PERF_FORMAT_GROUP | unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_ID
	insAttr.Sample_type = unix.PERF_SAMPLE_IDENTIFIER
	insAttr.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
	eventAttr["INST_RETIRED.ANY"] = insAttr
}

// test for MPKI
func NewPerfCoreCollector(core int, events []string) (*PerfCoreCollector, error) {
	pc := &PerfCoreCollector{
		Core:     core,
		otherFds: make([]io.ReadCloser, len(events)-1),
		id2event: make(map[uint64]string),
	}
	fd1, err := unix.PerfEventOpen(eventAttr["MEM_LOAD_RETIRED.L3_MISS"], -1, core, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	pc.leaderFd = os.NewFile(uintptr(fd1), "MEM_LOAD_RETIRED.L3_MISS")
	var id1 uint64
	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd1), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id1)))
	if err != syscall.Errno(0) {
		return nil, err
	}
	pc.id2event[id1] = "MEM_LOAD_RETIRED.L3_MISS"

	fd2, err := unix.PerfEventOpen(eventAttr["INST_RETIRED.ANY"], -1, core, fd1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	pc.otherFds[0] = os.NewFile(uintptr(fd2), "INST_RETIRED.ANY")
	var id2 uint64
	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd2), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id2)))
	if err != syscall.Errno(0) {
		return nil, err
	}
	pc.id2event[id2] = "INST_RETIRED.ANY"

	return pc, nil
}

func (p *PerfCoreCollector) Enable() error {
	leaderFd := int(p.leaderFd.(*os.File).Fd())
	_, _, e1 := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(leaderFd), uintptr(unix.PERF_EVENT_IOC_RESET), unix.PERF_IOC_FLAG_GROUP, 0, 0, 0)
	if e1 != syscall.Errno(0) {
		return fmt.Errorf("failed to reset perf event: %s", unix.ErrnoName(e1))
	}
	_, _, e1 = syscall.Syscall6(syscall.SYS_IOCTL, uintptr(leaderFd), uintptr(unix.PERF_EVENT_IOC_ENABLE), unix.PERF_IOC_FLAG_GROUP, 0, 0, 0)
	if e1 != syscall.Errno(0) {
		return fmt.Errorf("failed to reset perf event: %s", unix.ErrnoName(e1))
	}
	return nil
}

func (p *PerfCoreCollector) Collect() error {
	err := p.stop()
	if err != nil {
		klog.Error(err)
		return err
	}

	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)
	_, err = p.leaderFd.Read(*buf)
	if err != nil {
		klog.Error(err)
		return err
	}

	header := &GroupReadFormat{}
	reader := bytes.NewReader(*buf)
	if err := binary.Read(reader, binary.LittleEndian, header); err != nil {
		klog.Error(err)
		return err
	}

	scalingRatio := 1.0
	if header.TimeRunning != 0 && header.TimeEnabled != 0 {
		scalingRatio = float64(header.TimeRunning) / float64(header.TimeEnabled)
	}

	for i := 0; i < int(header.Nr); i++ {
		v := &perfValue{}
		if err := binary.Read(reader, binary.LittleEndian, v); err != nil {
			return err
		}
		metrics.RecordMPKI("inspur-icx-1", p.id2event[v.ID], p.Core, float64(v.Value)/scalingRatio)
	}

	return nil
}

func (p *PerfCoreCollector) stop() error {
	if err := unix.IoctlSetInt(int(p.leaderFd.(*os.File).Fd()), unix.PERF_EVENT_IOC_DISABLE, 1); err != nil {
		return err
	}
	return nil
}

func (p *PerfCoreCollector) Close() error {
	if err := p.leaderFd.Close(); err != nil {
		return err
	}
	for _, fd := range p.otherFds {
		if err := fd.Close(); err != nil {
			return err
		}
	}
	return nil
}
