package canokey

import (
	"encoding/binary"
	"unicode/utf16"

	"github.com/sagernet/sing-usbip"
	E "github.com/sagernet/sing/common/exceptions"
)

const (
	descriptorTypeDevice        = 1
	descriptorTypeConfiguration = 2
	descriptorTypeString        = 3
	descriptorTypeInterface     = 4
)

func (d *Device) queryDescriptor() (usbip.DeviceEntry, error) {
	deviceData, err := d.getDescriptor(descriptorTypeDevice, 0, 0, 18)
	if err != nil {
		return usbip.DeviceEntry{}, E.Cause(err, "get device descriptor")
	}
	if len(deviceData) < 18 {
		return usbip.DeviceEntry{}, E.New("short device descriptor: ", len(deviceData), " bytes")
	}
	info := usbip.DeviceInfoTruncated{
		Speed:              usbip.SpeedFull,
		BDeviceClass:       deviceData[4],
		BDeviceSubClass:    deviceData[5],
		BDeviceProtocol:    deviceData[6],
		IDVendor:           binary.LittleEndian.Uint16(deviceData[8:10]),
		IDProduct:          binary.LittleEndian.Uint16(deviceData[10:12]),
		BCDDevice:          binary.LittleEndian.Uint16(deviceData[12:14]),
		BNumConfigurations: deviceData[17],
	}

	header, err := d.getDescriptor(descriptorTypeConfiguration, 0, 0, 9)
	if err != nil {
		return usbip.DeviceEntry{}, E.Cause(err, "get configuration descriptor")
	}
	if len(header) < 9 {
		return usbip.DeviceEntry{}, E.New("short configuration descriptor: ", len(header), " bytes")
	}
	configuration, err := d.getDescriptor(descriptorTypeConfiguration, 0, 0, int(binary.LittleEndian.Uint16(header[2:4])))
	if err != nil {
		return usbip.DeviceEntry{}, E.Cause(err, "get configuration descriptor")
	}
	if len(configuration) < 9 {
		return usbip.DeviceEntry{}, E.New("short configuration descriptor: ", len(configuration), " bytes")
	}
	info.BConfigurationValue = configuration[5]
	interfaces := parseInterfaces(configuration)
	info.BNumInterfaces = uint8(len(interfaces))

	languageID := d.queryLanguageID()
	return usbip.DeviceEntry{
		Info:       info,
		Interfaces: interfaces,
		Serial:     d.queryString(deviceData[16], languageID),
		Product:    d.queryString(deviceData[15], languageID),
	}, nil
}

func (d *Device) getDescriptor(descriptorType uint8, index uint8, languageID uint16, length int) ([]byte, error) {
	value := uint16(descriptorType)<<8 | uint16(index)
	setup := [8]byte{0x80, 0x06}
	binary.LittleEndian.PutUint16(setup[2:4], value)
	binary.LittleEndian.PutUint16(setup[4:6], languageID)
	binary.LittleEndian.PutUint16(setup[6:8], uint16(length))
	response := d.control(setup, nil, length)
	if response.Status != 0 {
		return nil, E.New("URB status ", response.Status)
	}
	return response.Buffer, nil
}

func (d *Device) queryLanguageID() uint16 {
	data, err := d.getDescriptor(descriptorTypeString, 0, 0, 255)
	if err != nil || len(data) < 4 {
		return 0x0409
	}
	return binary.LittleEndian.Uint16(data[2:4])
}

func (d *Device) queryString(index uint8, languageID uint16) string {
	if index == 0 {
		return ""
	}
	data, err := d.getDescriptor(descriptorTypeString, index, languageID, 255)
	if err != nil {
		d.logger.Debug("get string descriptor ", index, ": ", err)
		return ""
	}
	if len(data) < 2 || data[1] != descriptorTypeString {
		return ""
	}
	length := min(int(data[0]), len(data))
	units := make([]uint16, 0, (length-2)/2)
	for offset := 2; offset+1 < length; offset += 2 {
		units = append(units, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}
	return string(utf16.Decode(units))
}

func parseInterfaces(configuration []byte) []usbip.DeviceInterface {
	var interfaces []usbip.DeviceInterface
	for offset := 0; offset+2 <= len(configuration); {
		length := int(configuration[offset])
		if length < 2 || offset+length > len(configuration) {
			break
		}
		if configuration[offset+1] == descriptorTypeInterface && length >= 9 && configuration[offset+3] == 0 {
			interfaces = append(interfaces, usbip.DeviceInterface{
				BInterfaceClass:    configuration[offset+5],
				BInterfaceSubClass: configuration[offset+6],
				BInterfaceProtocol: configuration[offset+7],
			})
		}
		offset += length
	}
	return interfaces
}
