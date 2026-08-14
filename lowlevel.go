package canokey

// #include <stdint.h>
import "C"

import "unsafe"

const statusOK = 0

//export USBD_LL_Init
func USBD_LL_Init(pdev unsafe.Pointer) C.int {
	return statusOK
}

//export USBD_LL_DeInit
func USBD_LL_DeInit(pdev unsafe.Pointer) C.int {
	return statusOK
}

//export USBD_LL_Start
func USBD_LL_Start(pdev unsafe.Pointer) C.int {
	instance.start()
	return statusOK
}

//export USBD_LL_Stop
func USBD_LL_Stop(pdev unsafe.Pointer) C.int {
	return statusOK
}

//export USBD_LL_OpenEP
func USBD_LL_OpenEP(pdev unsafe.Pointer, address C.uint8_t, kind C.uint8_t, maxPacket C.uint16_t) C.int {
	instance.openEndpoint(uint8(address), int(maxPacket))
	return statusOK
}

//export USBD_LL_CloseEP
func USBD_LL_CloseEP(pdev unsafe.Pointer, address C.uint8_t) C.int {
	instance.closeEndpoint(uint8(address))
	return statusOK
}

//export USBD_LL_FlushEP
func USBD_LL_FlushEP(pdev unsafe.Pointer, address C.uint8_t) C.int {
	instance.flushEndpoint(uint8(address))
	return statusOK
}

//export USBD_LL_StallEP
func USBD_LL_StallEP(pdev unsafe.Pointer, address C.uint8_t) C.int {
	instance.setStall(uint8(address), true)
	return statusOK
}

//export USBD_LL_ClearStallEP
func USBD_LL_ClearStallEP(pdev unsafe.Pointer, address C.uint8_t) C.int {
	instance.setStall(uint8(address), false)
	return statusOK
}

//export USBD_LL_IsStallEP
func USBD_LL_IsStallEP(pdev unsafe.Pointer, address C.uint8_t) C.uint8_t {
	if instance.isStalled(uint8(address)) {
		return 1
	}
	return 0
}

//export USBD_LL_SetUSBAddress
func USBD_LL_SetUSBAddress(pdev unsafe.Pointer, address C.uint8_t) C.int {
	return statusOK
}

//export USBD_LL_PrepareReceive
func USBD_LL_PrepareReceive(pdev unsafe.Pointer, address C.uint8_t, buffer unsafe.Pointer, size C.uint16_t) C.int {
	instance.prepareReceive(uint8(address), buffer, int(size))
	return statusOK
}

//export USBD_LL_Transmit
func USBD_LL_Transmit(pdev unsafe.Pointer, address C.uint8_t, buffer unsafe.Pointer, size C.uint16_t) C.int {
	instance.transmit(uint8(address), buffer, int(size))
	return statusOK
}

//export USBD_LL_GetRxDataSize
func USBD_LL_GetRxDataSize(pdev unsafe.Pointer, address C.uint8_t) C.uint32_t {
	return C.uint32_t(instance.receivedSize(uint8(address)))
}

//export USBD_LL_Delay
func USBD_LL_Delay(milliseconds C.uint32_t) {
}

//export USBD_LL_Init_Done
func USBD_LL_Init_Done() {
}

//export strong_user_presence_test
func strong_user_presence_test() C.int {
	if instance.userPresenceTest() {
		return 0
	}
	return -1
}

//export USBD_CTAPHID_ServiceReceive
func USBD_CTAPHID_ServiceReceive() {
	instance.serviceReceive()
}
