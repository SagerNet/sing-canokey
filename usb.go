package canokey

/*
#cgo CFLAGS: -I${SRCDIR}/canokey-core/include
#cgo CFLAGS: -I${SRCDIR}/canokey-core/littlefs
#cgo CFLAGS: -I${SRCDIR}/canokey-core/tinycbor/src
#cgo CFLAGS: -I${SRCDIR}/canokey-core/interfaces/USB/core/inc
#cgo CFLAGS: -I${SRCDIR}/canokey-core/interfaces/USB/device
#cgo CFLAGS: -I${SRCDIR}/canokey-core/virt-card
#cgo CFLAGS: -I${SRCDIR}/build/canokey-core
#cgo CFLAGS: -I${SRCDIR}/build/canokey-core/tinycbor
#cgo CFLAGS: -DENABLE_NFC=1 -DENABLE_APPLET_NDEF=1 -DENABLE_PASS=1
#cgo CFLAGS: -DENABLE_IFACE_CTAPHID=1 -DENABLE_IFACE_WEBUSB=1 -DENABLE_IFACE_CCID=1 -DENABLE_IFACE_KBDHID=1
#cgo CFLAGS: -Wno-macro-redefined
#cgo LDFLAGS: -L${SRCDIR}/build
#cgo LDFLAGS: -L${SRCDIR}/build/canokey-core
#cgo LDFLAGS: -L${SRCDIR}/build/canokey-core/canokey-crypto
#cgo LDFLAGS: -L${SRCDIR}/build/canokey-core/canokey-crypto/tf-psa-crypto/core

#include <stdio.h>
#include <stdlib.h>
#include <device.h>
#include <fabrication.h>
#include <usb_device.h>
#include <usbd_core.h>
#include <usbd_def.h>
*/
import "C"

import (
	"bytes"
	"time"
	"unsafe"

	"github.com/sagernet/sing-usbip"
	E "github.com/sagernet/sing/common/exceptions"
)

const (
	endpointCount    = 8
	maxEP0Size       = C.USB_MAX_EP0_SIZE
	defaultMaxPacket = 64
	maxQueuedPackets = 512
)

const (
	strongPresenceRounds = 5
	strongPresenceRound  = 2 * time.Second
)

type inEndpoint struct {
	maxPacket  int
	stalled    bool
	packets    [][]byte
	overflowed bool
}

type outEndpoint struct {
	maxPacket int
	stalled   bool
	buffer    unsafe.Pointer
	size      int
	count     int
}

func (d *Device) openEndpoint(address uint8, maxPacket int) {
	number := address & 0x7f
	if number >= endpointCount {
		return
	}
	if maxPacket == 0 {
		maxPacket = defaultMaxPacket
	}
	if address&0x80 != 0 {
		d.in[number] = inEndpoint{maxPacket: maxPacket}
	} else {
		d.out[number] = outEndpoint{maxPacket: maxPacket}
	}
}

func (d *Device) closeEndpoint(address uint8) {
	number := address & 0x7f
	if number >= endpointCount {
		return
	}
	if address&0x80 != 0 {
		d.in[number].packets = nil
		d.in[number].stalled = false
	} else {
		d.out[number].buffer = nil
		d.out[number].size = 0
		d.out[number].stalled = false
	}
}

func (d *Device) flushEndpoint(address uint8) {
	number := address & 0x7f
	if number >= endpointCount {
		return
	}
	if address&0x80 != 0 {
		d.in[number].packets = nil
	}
}

func (d *Device) setStall(address uint8, stalled bool) {
	number := address & 0x7f
	if number >= endpointCount {
		return
	}
	if address&0x80 != 0 {
		d.in[number].stalled = stalled
	} else {
		d.out[number].stalled = stalled
	}
}

func (d *Device) isStalled(address uint8) bool {
	number := address & 0x7f
	if number >= endpointCount {
		return false
	}
	if address&0x80 != 0 {
		return d.in[number].stalled
	}
	return d.out[number].stalled
}

func (d *Device) prepareReceive(address uint8, buffer unsafe.Pointer, size int) {
	number := address & 0x7f
	if number >= endpointCount {
		return
	}
	endpoint := &d.out[number]
	endpoint.buffer = buffer
	endpoint.size = size
}

func (d *Device) receivedSize(address uint8) int {
	number := address & 0x7f
	if number >= endpointCount {
		return 0
	}
	return d.out[number].count
}

func (d *Device) transmit(address uint8, buffer unsafe.Pointer, size int) {
	number := address & 0x7f
	if number >= endpointCount {
		return
	}
	d.transmitted = true
	endpoint := &d.in[number]
	if number == 0 {
		length := min(size, maxEP0Size)
		d.enqueue(endpoint, copyBytes(buffer, length))
		C.USBD_LL_DataInStage(&C.usb_device, 0, (*C.uint8_t)(unsafe.Add(buffer, length)))
		return
	}
	maxPacket := endpoint.maxPacket
	if maxPacket == 0 {
		maxPacket = defaultMaxPacket
	}
	if size == 0 {
		d.enqueue(endpoint, nil)
	}
	for offset := 0; offset < size; offset += maxPacket {
		d.enqueue(endpoint, copyBytes(unsafe.Add(buffer, offset), min(maxPacket, size-offset)))
	}
	C.USBD_LL_DataInStage(&C.usb_device, C.uint8_t(number), nil)
}

func (d *Device) enqueue(endpoint *inEndpoint, packet []byte) {
	if len(endpoint.packets) >= maxQueuedPackets {
		if !endpoint.overflowed {
			endpoint.overflowed = true
			d.logger.Warn("dropping device output: the host is not reading the endpoint")
		}
		return
	}
	endpoint.overflowed = false
	endpoint.packets = append(endpoint.packets, packet)
}

func (e *inEndpoint) drain(limit int) ([]byte, bool) {
	if len(e.packets) == 0 {
		return nil, false
	}
	var data []byte
	for len(e.packets) > 0 {
		packet := e.packets[0]
		if len(data)+len(packet) > limit {
			if len(data) > 0 {
				break
			}
			e.packets = e.packets[1:]
			return packet[:limit], true
		}
		e.packets = e.packets[1:]
		data = append(data, packet...)
		if len(packet) < e.maxPacket {
			break
		}
	}
	return data, true
}

func (d *Device) deliver(number uint8, data []byte) {
	endpoint := &d.out[number]
	maxPacket := endpoint.maxPacket
	if number == 0 {
		maxPacket = maxEP0Size
	}
	if maxPacket == 0 {
		maxPacket = defaultMaxPacket
	}
	for offset := 0; offset < len(data); {
		if endpoint.buffer == nil || endpoint.size == 0 {
			d.logger.Debug("dropping ", len(data)-offset, " bytes for endpoint ", number, ": no receive buffer")
			return
		}
		length := min(len(data)-offset, endpoint.size, maxPacket)
		copy(unsafe.Slice((*byte)(endpoint.buffer), endpoint.size), data[offset:offset+length])
		endpoint.count = length
		next := unsafe.Add(endpoint.buffer, length)
		endpoint.buffer = nil
		offset += length
		C.USBD_LL_DataOutStage(&C.usb_device, C.uint8_t(number), (*C.uint8_t)(next))
	}
}

func (d *Device) control(setup [8]byte, data []byte, length int) usbip.URBResponse {
	d.in[0].packets = nil
	d.in[0].stalled = false
	d.out[0].stalled = false
	C.USBD_LL_SetupStage(&C.usb_device, (*C.uint8_t)(unsafe.Pointer(&setup[0])))
	if setup[0]&0x80 == 0 && len(data) > 0 {
		d.deliver(0, data)
	}
	if d.in[0].stalled || d.out[0].stalled {
		return usbip.URBResponse{Status: statusEPIPE}
	}
	if setup[0]&0x80 == 0 {
		return usbip.URBResponse{ActualLength: int32(length)}
	}
	response, _ := d.in[0].drain(length)
	return usbip.URBResponse{Buffer: response, ActualLength: int32(len(response))}
}

func (d *Device) start() {
	C.USBD_LL_SetSpeed(&C.usb_device, C.USBD_SPEED_FULL)
	C.USBD_LL_Reset(&C.usb_device)
}

func (d *Device) initializeCore() error {
	C.setvbuf(C.stdout, nil, C._IONBF, 0)
	filePath := C.CString(d.options.FilePath)
	defer C.free(unsafe.Pointer(filePath))
	if d.fabricate {
		d.fabricating = true
		status := C.card_fabrication_procedure(filePath)
		d.fabricating = false
		if status != 0 {
			return E.New("fabricate canokey file: ", d.options.FilePath)
		}
	} else if C.card_read(filePath) != 0 {
		return E.New("read canokey file: ", d.options.FilePath)
	}
	C.usb_device_init()
	if d.options.Touch {
		C.set_nfc_state(0)
	} else {
		C.set_nfc_state(1)
	}
	setAddress := [8]byte{0x00, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00}
	result := d.control(setAddress, nil, 0)
	if result.Status != 0 {
		return E.New("set device address: URB status ", result.Status)
	}
	return nil
}

func (d *Device) ToggleTouch() bool {
	d.touchAccess.Lock()
	present := C.get_touch_result() == C.TOUCH_NO
	if present {
		C.set_touch_result(C.TOUCH_SHORT)
	} else {
		C.set_touch_result(C.TOUCH_NO)
	}
	d.touchAccess.Unlock()
	select {
	case d.touchSignal <- struct{}{}:
	default:
	}
	return present
}

func (d *Device) takeTouch() bool {
	d.touchAccess.Lock()
	defer d.touchAccess.Unlock()
	if C.get_touch_result() != C.TOUCH_SHORT {
		return false
	}
	C.set_touch_result(C.TOUCH_NO)
	return true
}

func (d *Device) userPresenceTest() bool {
	if d.fabricating || !d.options.Touch {
		return true
	}
	for round := 0; round < strongPresenceRounds; round++ {
		if !d.waitTouch() {
			return false
		}
	}
	return true
}

func (d *Device) waitTouch() bool {
	touched := d.takeTouch()
	round := time.NewTimer(strongPresenceRound)
	defer round.Stop()
	for {
		select {
		case <-d.touchSignal:
			if d.takeTouch() {
				touched = true
				round.Reset(strongPresenceRound)
			}
		case <-round.C:
			return touched
		}
	}
}

func copyBytes(pointer unsafe.Pointer, length int) []byte {
	if pointer == nil || length <= 0 {
		return nil
	}
	return bytes.Clone(unsafe.Slice((*byte)(pointer), length))
}
