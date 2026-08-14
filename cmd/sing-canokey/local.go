package main

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-usbip"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/spf13/cobra"
)

var localCommand = &cobra.Command{
	Use:   "local",
	Short: "Attach the device to this machine",
	Long: "Attach the device to this machine.\n\n" +
		"The device is exported by a USB/IP server bound to a loopback port and\n" +
		"imported from it by the USB/IP client of this machine.",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocal()
	},
}

func init() {
	mainCommand.AddCommand(localCommand)
}

func runLocal() error {
	contextLogger := log.StdLogger()
	device, err := startDevice(contextLogger)
	if err != nil {
		return err
	}
	defer device.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entry := device.DeviceEntry()
	if len(commandFlagID) >= len(entry.Info.BusID) {
		return E.New("device id too long: ", commandFlagID)
	}
	copy(entry.Info.BusID[:], commandFlagID)

	host := usbip.NewDynamicHost(contextLogger)
	server, err := usbip.NewDynamicServerService(ctx, usbip.ServerOptions{
		Logger: contextLogger,
		Listen: func(listenContext context.Context) (net.Listener, error) {
			var listenConfig net.ListenConfig
			return listenConfig.Listen(listenContext, N.NetworkTCP, "127.0.0.1:0")
		},
	}, host)
	if err != nil {
		return err
	}
	defer server.Close()
	err = server.Start()
	if err != nil {
		return err
	}
	busID, err := host.AddDevice(usbip.ProvidedDeviceInfo{Entry: entry}, device)
	if err != nil {
		return err
	}

	client, err := usbip.NewClientService(ctx, usbip.ClientOptions{
		Logger:        contextLogger,
		ServerAddress: M.SocksaddrFromNet(server.ListenAddr()),
		Devices:       []usbip.DeviceMatch{{BusID: busID}},
	})
	if err != nil {
		return err
	}
	defer client.Close()
	err = client.Start()
	if err != nil {
		return err
	}
	return waitInterrupt(contextLogger, device, nil, nil)
}
