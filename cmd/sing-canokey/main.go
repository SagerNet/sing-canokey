package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-canokey"
	"github.com/sagernet/sing/common/logger"

	"github.com/spf13/cobra"
)

const touchInterruptWindow = 2 * time.Second

var (
	commandFlagFile  string
	commandFlagID    string
	commandFlagTouch bool
)

var mainCommand = &cobra.Command{
	Use:   "sing-canokey",
	Short: "Virtual CanoKey provided over USB/IP",
	Long: "Virtual CanoKey provided over USB/IP.\n\n" +
		"The device is provided to a sing-box usbip-server, served by a standalone\n" +
		"USB/IP server, or attached to this machine, and lasts until the command is\n" +
		"interrupted.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	mainCommand.PersistentFlags().StringVar(&commandFlagFile, "file", "canokey-file", "Virtual CanoKey storage file")
	mainCommand.PersistentFlags().StringVar(&commandFlagID, "id", "canokey", "Device id, used as the bus id of the provided device")
	mainCommand.PersistentFlags().BoolVar(&commandFlagTouch, "touch", false, "Perform user presence tests, toggled by interrupt")
}

func main() {
	err := mainCommand.Execute()
	if err != nil {
		log.Fatal(unwrapStatus(err))
	}
}

func startDevice(contextLogger logger.ContextLogger) (*canokey.Device, error) {
	device, err := canokey.Start(canokey.Options{
		FilePath: commandFlagFile,
		Touch:    commandFlagTouch,
		Logger:   contextLogger,
	})
	if err != nil {
		return nil, err
	}
	if device.Fabricated() {
		contextLogger.Info("fabricated new canokey at ", commandFlagFile)
	} else {
		contextLogger.Info("loaded canokey from ", commandFlagFile)
	}
	return device, nil
}

func waitInterrupt(contextLogger logger.ContextLogger, device *canokey.Device, sessionDone <-chan error, onInterrupt func()) error {
	signalChan := make(chan os.Signal, 2)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)
	var lastInterrupt time.Time
	for {
		select {
		case signalValue := <-signalChan:
			if signalValue == os.Interrupt && commandFlagTouch && time.Since(lastInterrupt) > touchInterruptWindow {
				lastInterrupt = time.Now()
				if device.ToggleTouch() {
					contextLogger.Info("touch is now present, interrupt again quickly to quit")
				} else {
					contextLogger.Info("touch is now absent, interrupt again quickly to quit")
				}
				continue
			}
			if onInterrupt != nil {
				onInterrupt()
			}
			return nil
		case err := <-sessionDone:
			return err
		}
	}
}
