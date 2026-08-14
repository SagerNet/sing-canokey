package main

import (
	"context"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-usbip"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/spf13/cobra"
)

var commandFlagListen string

var serverCommand = &cobra.Command{
	Use:   "server",
	Short: "Serve the device as a USB/IP server",
	Long: "Serve the device as a USB/IP server.\n\n" +
		"The device is exported by a USB/IP server bound to the listen address and\n" +
		"can be imported by any USB/IP client, such as\n" +
		"`usbip attach -r <host> -b <id>`.",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer()
	},
}

func init() {
	serverCommand.Flags().StringVar(&commandFlagListen, "listen", "0.0.0.0:3240", "Listen address")
	mainCommand.AddCommand(serverCommand)
}

func runServer() error {
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

	listenAddress := M.ParseSocksaddr(commandFlagListen)
	if listenAddress.Port == 0 {
		listenAddress.Port = usbip.DefaultPort
	}

	host := usbip.NewDynamicHost(contextLogger)
	server, err := usbip.NewDynamicServerService(ctx, usbip.ServerOptions{
		Logger:        contextLogger,
		ListenAddress: listenAddress,
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
	contextLogger.Info("serving ", busID, " at ", server.ListenAddr())
	return waitInterrupt(contextLogger, device, nil, nil)
}
