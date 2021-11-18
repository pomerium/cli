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
