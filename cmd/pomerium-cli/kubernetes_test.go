package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestKubernetesFlushCredentialsCmd(t *testing.T) {
	t.Run("TestK8slushCredentialsCmd", func(t *testing.T) {
		// Capture stdout to a buffer
		var stdoutBuf bytes.Buffer
		cmd := kubernetesFlushCredentialsCmd
		cmd.SetOutput(&stdoutBuf)

		args := []string{"k8s", "http://k8s/example.com"}
		cmd.SetArgs(args)

		// Execute the command
		err := cmd.Execute()
		assert.NoError(t, err)

		actualOutput := stdoutBuf.String()
		assert.NotNil(t, actualOutput)

	})
}

func TestParseToken(t *testing.T) {
	rawjwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	expectedExecCred := &ExecCredential{
		TypeMeta: TypeMeta{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Kind:       "ExecCredential",
		},
		Status: &ExecCredentialStatus{
			Token: "Pomerium-eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		},
	}

	execCred, err := parseToken(rawjwt)
	assert.NoError(t, err)
	assert.Equal(t, expectedExecCred.TypeMeta, execCred.TypeMeta)
	assert.Equal(t, expectedExecCred.Status.Token, execCred.Status.Token)
	// Check if ExpirationTimestamp is set within a reasonable range.
	assert.WithinDuration(t, time.Now().Add(time.Hour), execCred.Status.ExpirationTimestamp, time.Second)
}

func TestPrintCreds(t *testing.T) {
	testCreds := &ExecCredential{
		TypeMeta: TypeMeta{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Kind:       "ExecCredential",
		},
	}
	printCreds(testCreds)
}
