package api

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/pomerium/cli/proto"
)

func TestConfig(t *testing.T) {
	cfg := NewConfig()

	assert.Empty(t, cfg.getTags())
	assert.Empty(t, cfg.listAll())

	cfg.upsert(&pb.Record{
		Id:   proto.String("a"),
		Tags: []string{"alpha", "bravo"},
	})
	cfg.upsert(&pb.Record{
		Id:   proto.String("b"),
		Tags: []string{"go", "bravo"},
	})
	assert.Empty(t, cmp.Diff([]string{"alpha", "bravo", "go"},
		cfg.getTags(),
		cmpopts.SortSlices(func(a, b string) bool { return a < b })))

	require.NoError(t, cfg.delete("a"))
	assert.Empty(t, cmp.Diff([]string{"bravo", "go"},
		cfg.getTags(),
		cmpopts.SortSlices(func(a, b string) bool { return a < b })))

	require.NoError(t, cfg.delete("b"))
	assert.Empty(t, cfg.getTags())
	assert.Empty(t, cfg.listAll())
}

func TestTags(t *testing.T) {
	cfg := NewConfig()

	require.Empty(t, cfg.getTags())
	require.Empty(t, cfg.listAll())

	for _, tags := range [][]string{
		{"alpha", "bravo"},
		{"alpha"},
		{},
		{"charlie", "delta"},
		{"echo"},
		{"echo", "foxtrot"},
	} {
		cfg.upsert(&pb.Record{
			Id:   proto.String("a"),
			Tags: tags,
		})
		assert.Empty(t, cmp.Diff(tags, cfg.getTags(),
			cmpopts.SortSlices(func(a, b string) bool { return a < b })), tags)
	}
}

func TestAssignID(t *testing.T) {
	cfg := NewConfig()

	assert.Empty(t, cfg.getTags())
	assert.Empty(t, cfg.listAll())

	rec := &pb.Record{
		Tags: []string{"alpha"},
	}
	cfg.upsert(rec)
	assert.NotEmpty(t, rec.Id)
}

const exampleConfig = `{
  "@type": "type.googleapis.com/pomerium.cli.Records",
  "records": [
    {
      "id": "114acc22-c18f-4326-8606-425acc2b3eb5",
	  "tags": ["admin"],
      "conn": {
        "name": "Example",
        "remoteAddr": "example.route.pomerium.com:5000",
        "listenAddr": "127.0.0.1:5000",
        "disableTlsVerification": false,
        "foo": "bar"
      }
    }
  ]
}`

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(&stubConfigProvider{[]byte(exampleConfig)})
	assert.NoError(t, err)

	exampleRecord := &pb.Record{
		Id:   ptr("114acc22-c18f-4326-8606-425acc2b3eb5"),
		Tags: []string{"admin"},
		Conn: &pb.Connection{
			Name:       ptr("Example"),
			RemoteAddr: "example.route.pomerium.com:5000",
			ListenAddr: ptr("127.0.0.1:5000"),
			TlsOptions: &pb.Connection_DisableTlsVerification{},
		},
	}

	assert.Equal(t, &config{
		byID: map[string]*pb.Record{
			"114acc22-c18f-4326-8606-425acc2b3eb5": exampleRecord,
		},
		byTag: map[string]map[string]*pb.Record{
			"admin": {
				"114acc22-c18f-4326-8606-425acc2b3eb5": exampleRecord,
			},
		},
	}, cfg)
}

type stubConfigProvider struct {
	data []byte
}

func (s *stubConfigProvider) Load() ([]byte, error) {
	return s.data, nil
}

func (s *stubConfigProvider) Save(b []byte) error {
	s.data = b
	return nil
}

func ptr[T any](value T) *T {
	return &value
}
