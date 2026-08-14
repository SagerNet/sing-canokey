package main

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/daemon"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-canokey"
	"github.com/sagernet/sing-usbip"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const detachTimeout = 2 * time.Second

var (
	commandFlagURL    string
	commandFlagSecret string
	commandFlagServer string
)

var provideCommand = &cobra.Command{
	Use:   "provide",
	Short: "Provide the device to a sing-box usbip-server",
	Long: "Provide the device to a sing-box usbip-server.\n\n" +
		"The device is provided through the sing-box API service to a usbip-server\n" +
		"with the dynamic provider.",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProvide()
	},
}

func init() {
	provideCommand.Flags().StringVar(&commandFlagURL, "url", "", "API service URL (default: $BOX_API_URL)")
	provideCommand.Flags().StringVar(&commandFlagSecret, "secret", "", "API service secret (default: $BOX_API_SECRET)")
	provideCommand.Flags().StringVar(&commandFlagServer, "server", "", "usbip-server tag (default: the only usbip-server with dynamic provider)")
	mainCommand.AddCommand(provideCommand)
}

func runProvide() error {
	contextLogger := log.StdLogger()
	device, err := startDevice(contextLogger)
	if err != nil {
		return err
	}
	defer device.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := dialProvider(ctx, contextLogger, device)
	if err != nil {
		return err
	}
	defer session.Close()

	sessionDone := make(chan error, 1)
	go func() {
		runErr := session.Run()
		if runErr == nil {
			runErr = E.New("provider stream closed")
		}
		sessionDone <- runErr
	}()
	return waitInterrupt(contextLogger, device, sessionDone, session.Detach)
}

func unwrapStatus(err error) error {
	grpcStatus, isStatus := status.FromError(err)
	if !isStatus {
		return err
	}
	switch grpcStatus.Code() {
	case codes.Unavailable:
		return E.New("failed to connect to API service at ", apiServerURL, ": ", grpcStatus.Message())
	case codes.Unknown:
		return E.New(grpcStatus.Message())
	case codes.Unimplemented:
		return E.New(grpcStatus.Code().String(), ": ", grpcStatus.Message(), " (client API version ", daemon.APIVersion, ")")
	default:
		return E.New(grpcStatus.Code().String(), ": ", grpcStatus.Message())
	}
}

type providerSession struct {
	ctx        context.Context
	cancel     context.CancelFunc
	logger     logger.ContextLogger
	device     *canokey.Device
	clientConn *grpc.ClientConn
	serverTag  string
	deviceID   string
	stream     daemon.StartedService_ProvideUSBDevicesClient

	sendAccess   sync.Mutex
	seenInStatus bool
	detached     chan struct{}
	finished     chan struct{}
}

var apiServerURL string

func createAPIClient() (*grpc.ClientConn, daemon.StartedServiceClient, error) {
	serverURL := commandFlagURL
	if serverURL == "" {
		serverURL = os.Getenv("BOX_API_URL")
	}
	if serverURL == "" {
		return nil, nil, E.New("missing API service URL, set --url or BOX_API_URL")
	}
	if !strings.Contains(serverURL, "://") {
		serverURL = "http://" + serverURL
	}
	apiServerURL = serverURL
	secret := commandFlagSecret
	if secret == "" {
		secret = os.Getenv("BOX_API_SECRET")
	}
	clientConn, err := daemon.NewRemoteClient(daemon.RemoteClientOptions{
		ServerURL: serverURL,
		Secret:    secret,
	})
	if err != nil {
		return nil, nil, err
	}
	return clientConn, daemon.NewStartedServiceClient(clientConn), nil
}

func dialProvider(ctx context.Context, contextLogger logger.ContextLogger, device *canokey.Device) (*providerSession, error) {
	clientConn, client, err := createAPIClient()
	if err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &providerSession{
		ctx:        sessionCtx,
		cancel:     cancel,
		logger:     contextLogger,
		device:     device,
		clientConn: clientConn,
		deviceID:   commandFlagID,
		detached:   make(chan struct{}),
		finished:   make(chan struct{}),
	}
	statusStream, err := client.SubscribeUSBIPServerStatus(sessionCtx, &emptypb.Empty{})
	if err != nil {
		session.Close()
		return nil, err
	}
	update, err := statusStream.Recv()
	if err != nil {
		session.Close()
		return nil, err
	}
	server, err := resolveServer(update.GetServers(), commandFlagServer)
	if err != nil {
		session.Close()
		return nil, err
	}
	session.serverTag = server.GetServerTag()
	session.stream, err = client.ProvideUSBDevices(sessionCtx)
	if err != nil {
		session.Close()
		return nil, err
	}
	err = session.attach()
	if err != nil {
		session.Close()
		return nil, err
	}
	go session.readStatus(statusStream)
	return session, nil
}

func resolveServer(servers []*daemon.USBIPServerStatus, tag string) (*daemon.USBIPServerStatus, error) {
	if tag != "" {
		index := slices.IndexFunc(servers, func(it *daemon.USBIPServerStatus) bool {
			return it.GetServerTag() == tag
		})
		if index == -1 {
			return nil, E.New("usbip-server not found: ", tag, serverTagHint(servers))
		}
		return servers[index], nil
	}
	switch len(servers) {
	case 0:
		return nil, E.New("no usbip-server with dynamic provider found")
	case 1:
		return servers[0], nil
	default:
		return nil, E.New("multiple usbip-servers found, select one with --server", serverTagHint(servers))
	}
}

func serverTagHint(servers []*daemon.USBIPServerStatus) string {
	if len(servers) == 0 {
		return ""
	}
	return " (known tags: " + strings.Join(common.Map(servers, func(it *daemon.USBIPServerStatus) string {
		return it.GetServerTag()
	}), ", ") + ")"
}

func (s *providerSession) attach() error {
	entry := s.device.DeviceEntry()
	return s.send(&daemon.USBProviderMessage{Message: &daemon.USBProviderMessage_Attach{Attach: &daemon.USBDeviceAttach{
		ServerTag: s.serverTag,
		Descriptor_: &daemon.USBDeviceDescriptor{
			DeviceId:           s.deviceID,
			Speed:              entry.Info.Speed,
			VendorId:           uint32(entry.Info.IDVendor),
			ProductId:          uint32(entry.Info.IDProduct),
			BcdDevice:          uint32(entry.Info.BCDDevice),
			DeviceClass:        uint32(entry.Info.BDeviceClass),
			DeviceSubClass:     uint32(entry.Info.BDeviceSubClass),
			DeviceProtocol:     uint32(entry.Info.BDeviceProtocol),
			ConfigurationValue: uint32(entry.Info.BConfigurationValue),
			NumConfigurations:  uint32(entry.Info.BNumConfigurations),
			Interfaces: common.Map(entry.Interfaces, func(it usbip.DeviceInterface) *daemon.USBInterface {
				return &daemon.USBInterface{
					InterfaceClass:    uint32(it.BInterfaceClass),
					InterfaceSubClass: uint32(it.BInterfaceSubClass),
					InterfaceProtocol: uint32(it.BInterfaceProtocol),
				}
			}),
			Serial:  entry.Serial,
			Product: entry.Product,
		},
	}}})
}

func (s *providerSession) send(message *daemon.USBProviderMessage) error {
	s.sendAccess.Lock()
	defer s.sendAccess.Unlock()
	return s.stream.Send(message)
}

func (s *providerSession) Run() error {
	defer close(s.finished)
	for {
		message, err := s.stream.Recv()
		if err != nil {
			select {
			case <-s.detached:
				return nil
			default:
			}
			if s.ctx.Err() != nil {
				return nil
			}
			if err == io.EOF {
				return E.New("provider stream closed by the API service")
			}
			return err
		}
		switch body := message.GetMessage().(type) {
		case *daemon.USBServerMessage_Ready:
			s.logger.Info("shared ", body.Ready.GetBusId(), " on ", s.serverTag)
		case *daemon.USBServerMessage_UrbRequest:
			go s.transfer(body.UrbRequest)
		case *daemon.USBServerMessage_Abort:
			_ = s.device.AbortEndpoint(uint8(body.Abort.GetEndpoint()))
		case *daemon.USBServerMessage_Error:
			failureMessage := body.Error.GetMessage()
			if strings.Contains(failureMessage, "dynamic device already provided:") {
				return E.New(failureMessage, " (select another device id with --id)")
			}
			return E.New(failureMessage)
		}
	}
}

func (s *providerSession) transfer(request *daemon.USBURBRequest) {
	var setup [8]byte
	copy(setup[:], request.GetSetup())
	result := s.device.Submit(usbip.URBRequest{
		Command: usbip.SubmitCommand{
			TransferBufferLength: int32(request.GetTransferBufferLength()),
			Setup:                setup,
		},
		Endpoint: uint8(request.GetEndpoint()),
		Buffer:   request.GetOutData(),
	})
	response := &daemon.USBURBResponse{
		DeviceId:     request.GetDeviceId(),
		Seq:          request.GetSeq(),
		Status:       result.Status,
		ActualLength: result.ActualLength,
	}
	if request.GetDirectionIn() {
		response.InData = result.Buffer
	}
	err := s.send(&daemon.USBProviderMessage{Message: &daemon.USBProviderMessage_UrbResponse{UrbResponse: response}})
	if err != nil && s.ctx.Err() == nil {
		s.logger.Debug("send URB response: ", err)
	}
}

// The device is dropped from the server when its service is restarted.
func (s *providerSession) readStatus(stream daemon.StartedService_SubscribeUSBIPServerStatusClient) {
	for {
		update, err := stream.Recv()
		if err != nil {
			return
		}
		index := slices.IndexFunc(update.GetServers(), func(it *daemon.USBIPServerStatus) bool {
			return it.GetServerTag() == s.serverTag
		})
		if index == -1 {
			continue
		}
		present := slices.ContainsFunc(update.GetServers()[index].GetDevices(), func(it *daemon.USBSharedDevice) bool {
			return it.GetBusId() == s.deviceID
		})
		if present {
			s.seenInStatus = true
			continue
		}
		if !s.seenInStatus {
			continue
		}
		select {
		case <-s.detached:
			return
		default:
		}
		s.seenInStatus = false
		s.logger.Warn("device dropped by ", s.serverTag, ", providing it again")
		err = s.attach()
		if err != nil {
			s.logger.Error("provide device: ", err)
			return
		}
	}
}

func (s *providerSession) Detach() {
	close(s.detached)
	err := s.send(&daemon.USBProviderMessage{Message: &daemon.USBProviderMessage_Detach{
		Detach: &daemon.USBDeviceDetach{DeviceId: s.deviceID},
	}})
	if err != nil {
		return
	}
	_ = s.stream.CloseSend()
	timer := time.NewTimer(detachTimeout)
	defer timer.Stop()
	select {
	case <-s.finished:
	case <-timer.C:
	case <-s.ctx.Done():
	}
}

func (s *providerSession) Close() error {
	s.cancel()
	return s.clientConn.Close()
}
