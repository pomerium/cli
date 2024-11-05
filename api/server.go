package api

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/golang/groupcache/lru"

	pb "github.com/pomerium/cli/proto"
	"github.com/pomerium/cli/tunnel"
)

// ConfigProvider provides interface to the configuration persistence
type ConfigProvider interface {
	// Load returns configuration data,
	// should not throw an error if underlying storage does not exist
	Load() ([]byte, error)
	// Save stores data into storage
	Save([]byte) error
}

type Config interface{}

// ListenerStatus marks individual records as locked
type ListenerStatus interface {
	// Lock marks a particular ID locked and provides a function to be called on unlock
	SetListening(id string, onUnlock context.CancelFunc, addr string) error
	// IsListening checks whether particular ID is currently locked
	GetListenerStatus(id string) *pb.ListenerStatus
	// Unlock unlocks the ID and calls onUnlock function and clears listener status
	SetNotListening(id string) error
	// SetListenError sets listener status to an error
	SetListenerError(id string, err error) error
}

// Tunnel is abstraction over tunnel.Tunnel to allow mocking
type Tunnel interface {
	Run(context.Context, io.ReadWriter, tunnel.EventSink) error
}

// Server implements both config and listener interfaces
type Server interface {
	pb.ConfigServer
	pb.ListenerServer
}

type server struct {
	sync.RWMutex
	ConfigProvider
	EventBroadcaster
	ListenerStatus
	*config
	browserCmd         string
	serviceAccount     string
	serviceAccountFile string
	certInfo           *lru.Cache
}

var (
	errNotFound         = errors.New("not found")
	errAlreadyListening = errors.New("already listening")
	errNotListening     = errors.New("not listening")
)

// NewServer creates new configuration management server
func NewServer(ctx context.Context, opts ...ServerOption) (Server, error) {
	srv := &server{
		ListenerStatus:   newListenerStatus(),
		EventBroadcaster: NewEventsBroadcaster(ctx),
		certInfo:         lru.New(256),
	}

	for _, opt := range append(opts,
		withDefaultConfigProvider(),
	) {
		if err := opt(srv); err != nil {
			return nil, err
		}
	}

	return srv, nil
}

// ServerOption allows to customize certain behavior
type ServerOption func(*server) error

// WithConfigProvider customizes configuration persistence
func WithConfigProvider(cp ConfigProvider) ServerOption {
	return func(s *server) error {
		cfg, err := loadConfig(cp)
		if err != nil {
			return err
		}
		s.config = cfg
		s.ConfigProvider = cp
		return nil
	}
}

func withDefaultConfigProvider() ServerOption {
	return func(s *server) error {
		if s.ConfigProvider == nil {
			return WithConfigProvider(new(MemCP))(s)
		}
		return nil
	}
}

func WithBrowserCommand(cmd string) ServerOption {
	return func(s *server) error {
		s.browserCmd = cmd
		return nil
	}
}

func WithServiceAccount(serviceAccount string) ServerOption {
	return func(s *server) error {
		s.serviceAccount = serviceAccount
		return nil
	}
}

func WithServiceAccountFile(serviceAccountFile string) ServerOption {
	return func(s *server) error {
		s.serviceAccountFile = serviceAccountFile
		return nil
	}
}

// MemCP is in-memory config provider
type MemCP struct {
	data []byte
}

// Load loads the configuration data
func (s *MemCP) Load() ([]byte, error) {
	return s.data, nil
}

// Save saves configuration data
func (s *MemCP) Save(data []byte) error {
	s.data = data
	return nil
}
