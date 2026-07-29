package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandSkill(t *testing.T) {
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		printSkill = false
	})

	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetArgs([]string{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Usage:", "--skill"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help output missing %q", want)
		}
	}
	if rootCmd.PersistentFlags().Lookup("skill") != nil {
		t.Fatal("--skill must not be persistent")
	}

	output.Reset()
	rootCmd.SetArgs([]string{"--skill"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), string(skillMD); got != want {
		t.Fatalf("--skill output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
