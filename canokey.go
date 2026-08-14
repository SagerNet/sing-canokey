package canokey

// #include <device.h>
import "C"

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sagernet/sing-usbip"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
)

const (
	statusEIO        = -5
	statusEPIPE      = -32
	statusECONNRESET = -104
	statusESHUTDOWN  = -108
)

// canokey-core only emits the CTAPHID ERR_MSG_TIMEOUT response when device_loop runs after
// CTAPHID_TRANS_TIMEOUT (800 ms) has passed since the OUT frame that left a message incomplete.
const transactionTimeout = 850 * time.Millisecond

type Options struct {
	FilePath string
	Touch    bool
	Logger   logger.Logger
}

var instance *Device

type Device struct {
	options   Options
	logger    logger.Logger
	fabricate bool
	entry     usbip.DeviceEntry

	jobs        chan any
	touchSignal chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	touchAccess sync.Mutex

	transactionDeadline *time.Timer

	fabricating bool
	inService   bool
	transmitted bool

	in     [endpointCount]inEndpoint
	out    [endpointCount]outEndpoint
	parked [endpointCount]*submitJob
}

type submitJob struct {
	request usbip.URBRequest
	result  chan usbip.URBResponse
}

type abortJob struct {
	endpoint uint8
}

func (j *submitJob) complete(response usbip.URBResponse) {
	select {
	case j.result <- response:
	default:
	}
}

func Start(options Options) (*Device, error) {
	if options.Logger == nil {
		options.Logger = logger.NOP()
	}
	if instance != nil {
		return nil, E.New("canokey device already started")
	}
	device := &Device{
		options:     options,
		logger:      options.Logger,
		jobs:        make(chan any),
		touchSignal: make(chan struct{}, 1),
		done:        make(chan struct{}),
	}
	_, err := os.Stat(options.FilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, E.Cause(err, "access canokey file")
		}
		err = os.MkdirAll(filepath.Dir(options.FilePath), 0o700)
		if err != nil {
			return nil, E.Cause(err, "create canokey file directory")
		}
		device.fabricate = true
	}
	instance = device
	started := make(chan error, 1)
	go device.run(started)
	err = <-started
	if err != nil {
		instance = nil
		return nil, err
	}
	return device, nil
}

func (d *Device) DeviceEntry() usbip.DeviceEntry {
	return d.entry
}

func (d *Device) Fabricated() bool {
	return d.fabricate
}

func (d *Device) Submit(request usbip.URBRequest) usbip.URBResponse {
	job := &submitJob{request: request, result: make(chan usbip.URBResponse, 1)}
	select {
	case d.jobs <- job:
	case <-d.done:
		return usbip.URBResponse{Status: statusESHUTDOWN}
	}
	select {
	case response := <-job.result:
		return response
	case <-d.done:
		return usbip.URBResponse{Status: statusESHUTDOWN}
	}
}

func (d *Device) AbortEndpoint(endpoint uint8) error {
	select {
	case d.jobs <- abortJob{endpoint: endpoint}:
	case <-d.done:
	}
	return nil
}

func (d *Device) Close() error {
	d.closeOnce.Do(func() {
		close(d.done)
	})
	return nil
}

func (d *Device) run(started chan error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := d.initializeCore()
	if err == nil {
		d.entry, err = d.queryDescriptor()
	}
	started <- err
	if err != nil {
		return
	}
	d.transactionDeadline = time.NewTimer(transactionTimeout)
	d.transactionDeadline.Stop()
	defer d.transactionDeadline.Stop()
	defer d.shutdown()
	for {
		select {
		case job := <-d.jobs:
			d.handle(job)
			d.pump()
		case <-d.touchSignal:
			d.pump()
		case <-d.transactionDeadline.C:
			d.pump()
		case <-d.done:
			return
		}
	}
}

func (d *Device) handle(job any) {
	switch body := job.(type) {
	case *submitJob:
		d.submit(body)
	case abortJob:
		number := body.endpoint & 0x7f
		if number >= endpointCount {
			return
		}
		parked := d.parked[number]
		if parked != nil {
			d.parked[number] = nil
			parked.complete(usbip.URBResponse{Status: statusECONNRESET})
		}
	}
}

func (d *Device) submit(job *submitJob) {
	request := job.request
	length := int(request.Command.TransferBufferLength)
	number := request.Endpoint & 0x7f
	if number >= endpointCount {
		job.complete(usbip.URBResponse{Status: statusEPIPE})
		return
	}
	if number == 0 {
		job.complete(d.control(request.Command.Setup, request.Buffer, length))
		return
	}
	if d.isStalled(request.Endpoint) {
		job.complete(usbip.URBResponse{Status: statusEPIPE})
		return
	}
	if request.Endpoint&0x80 == 0 {
		d.deliver(number, request.Buffer)
		d.transactionDeadline.Reset(transactionTimeout)
		job.complete(usbip.URBResponse{ActualLength: request.Command.TransferBufferLength})
		return
	}
	data, drained := d.in[number].drain(length)
	if drained {
		job.complete(usbip.URBResponse{Buffer: data, ActualLength: int32(len(data))})
		return
	}
	parked := d.parked[number]
	if parked != nil {
		parked.complete(usbip.URBResponse{Status: statusEIO})
	}
	d.parked[number] = job
}

func (d *Device) pump() {
	for {
		d.transmitted = false
		C.device_loop()
		d.completeParked()
		if !d.transmitted {
			return
		}
	}
}

func (d *Device) serviceReceive() {
	if d.inService {
		return
	}
	d.inService = true
	defer func() { d.inService = false }()
	for {
		select {
		case job := <-d.jobs:
			d.handle(job)
		default:
			d.completeParked()
			return
		}
	}
}

func (d *Device) completeParked() {
	for number := range d.parked {
		job := d.parked[number]
		if job == nil {
			continue
		}
		data, drained := d.in[number].drain(int(job.request.Command.TransferBufferLength))
		if !drained {
			continue
		}
		d.parked[number] = nil
		job.complete(usbip.URBResponse{Buffer: data, ActualLength: int32(len(data))})
	}
}

func (d *Device) shutdown() {
	for number := range d.parked {
		job := d.parked[number]
		if job == nil {
			continue
		}
		d.parked[number] = nil
		job.complete(usbip.URBResponse{Status: statusESHUTDOWN})
	}
}
