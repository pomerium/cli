package version

import (
	"fmt"
	"runtime"
	"testing"
)

func TestFullVersionVersion(t *testing.T) {
	tests := []struct {
		Version   string
		GitCommit string
		BuildMeta string
		Features  []string

		expected string
	}{
		{"", "", "", nil, ""},
		{"1.0.0", "", "", nil, "1.0.0"},
		{"1.0.0", "314501b", "", nil, "1.0.0+314501b"},
		{"1.0.0", "314501b", "dev", nil, "1.0.0-dev+314501b"},
		{"1.0.0", "314501b", "dev", []string{"foo", "bar"}, "1.0.0-dev+314501b\nFeatures: foo bar"},
	}
	for _, tt := range tests {
		Version = tt.Version
		GitCommit = tt.GitCommit
		BuildMeta = tt.BuildMeta
		Features = tt.Features

		if got := FullVersion(); got != tt.expected {
			t.Errorf("expected (%s) got (%s) for Version(%s), GitCommit(%s) BuildMeta(%s) Features(%v)",
				tt.expected,
				got,
				tt.Version,
				tt.GitCommit,
				tt.BuildMeta,
				tt.Features)
		}
	}
}

func BenchmarkFullVersion(b *testing.B) {
	Version = "1.0.0"
	GitCommit = "314501b"
	BuildMeta = "dev"
	for i := 0; i < b.N; i++ {
		FullVersion()
	}
}

func TestUserAgent(t *testing.T) {
	tests := []struct {
		name        string
		Version     string
		GitCommit   string
		BuildMeta   string
		ProjectName string
		ProjectURL  string
		want        string
	}{
		{"good user agent", "1.0.0", "314501b", "dev", "pomerium", "github.com/pomerium", fmt.Sprintf("pomerium/1.0.0 (+github.com/pomerium; 314501b; %s)", runtime.Version())},
	}
	for _, tt := range tests {
		Version = tt.Version
		GitCommit = tt.GitCommit
		BuildMeta = tt.BuildMeta
		ProjectName = tt.ProjectName
		ProjectURL = tt.ProjectURL

		t.Run(tt.name, func(t *testing.T) {
			if got := UserAgent(); got != tt.want {
				t.Errorf("UserAgent() = %v, want %v", got, tt.want)
			}
		})
	}
}
