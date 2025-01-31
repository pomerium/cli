package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/spf13/cobra"

	"github.com/pomerium/cli/authclient"
)

func init() {
	addBrowserFlags(kubernetesExecCredentialCmd)
	addServiceAccountFlags(kubernetesExecCredentialCmd)
	addTLSFlags(kubernetesExecCredentialCmd)
	kubernetesCmd.AddCommand(kubernetesExecCredentialCmd)
	kubernetesCmd.AddCommand(kubernetesFlushCredentialsCmd)
	rootCmd.AddCommand(kubernetesCmd)
}

var kubernetesCmd = &cobra.Command{
	Use:   "k8s",
	Short: "commands for the kubernetes credential plugin",
}

var kubernetesFlushCredentialsCmd = &cobra.Command{
	Use:   "flush-credentials [API Server URL]",
	Short: "clear the kubernetes credentials for the given URL to force a new login",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return clearAllCachedCredentials()
		} else {
			return clearCachedCredential(args[0])
		}
	},
}

var kubernetesExecCredentialCmd = &cobra.Command{
	Use:   "exec-credential",
	Short: "run the kubernetes credential plugin for use with kubectl",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("server url is required")
		}

		cacheLastURL(args[0])

		serverURL, err := url.Parse(args[0])
		if err != nil {
			return fmt.Errorf("invalid server url: %v", err)
		}

		var tlsConfig *tls.Config
		if serverURL.Scheme == "https" {
			tlsConfig, err = getTLSConfig()
			if err != nil {
				return err
			}
		}

		ac := authclient.New(
			authclient.WithBrowserCommand(browserOptions.command),
			authclient.WithServiceAccount(serviceAccountOptions.serviceAccount),
			authclient.WithServiceAccountFile(serviceAccountOptions.serviceAccountFile),
			authclient.WithTLSConfig(tlsConfig))

		creds, err := loadCachedCredential(serverURL.String())
		if err == nil && ac.CheckBearerToken(context.Background(), serverURL, creds.Status.Token) == nil {
			printCreds(creds)
			return nil
		}

		rawJWT, err := ac.GetJWT(context.Background(), serverURL, func(s string) {})
		if err != nil {
			fatalf("%s", err)
		}

		creds, err = parseToken(rawJWT)
		if err != nil {
			return err
		}

		if err = saveCachedCredential(serverURL.String(), creds); err != nil {
			return err
		}
		printCreds(creds)

		return nil
	},
}

func parseToken(rawjwt string) (*ExecCredential, error) {
	tok, err := jose.ParseSigned(rawjwt)
	if err != nil {
		return nil, err
	}

	var claims struct {
		Expiry int64 `json:"exp"`
	}
	err = json.Unmarshal(tok.UnsafePayloadWithoutVerification(), &claims)
	if err != nil {
		return nil, err
	}

	var expiresAt time.Time
	if claims.Expiry != 0 {
		expiresAt = time.Unix(claims.Expiry, 0)
	}

	return &ExecCredential{
		TypeMeta: TypeMeta{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Kind:       "ExecCredential",
		},
		Status: &ExecCredentialStatus{
			ExpirationTimestamp: expiresAt,
			Token:               "Pomerium-" + rawjwt,
		},
	}, nil
}

func printCreds(creds *ExecCredential) {
	bs, err := json.Marshal(creds)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode credentials: %v\n", err)
	}
	fmt.Println(string(bs))
}

// TypeMeta describes an individual object in an API response or request
// with strings representing the type of the object and its API schema version.
// Structures that are versioned or persisted should inline TypeMeta.
//
// +k8s:deepcopy-gen=false
type TypeMeta struct {
	// Kind is a string value representing the REST resource this object represents.
	// Servers may infer this from the endpoint the client submits requests to.
	// Cannot be updated.
	// In CamelCase.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	Kind string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`

	// APIVersion defines the versioned schema of this representation of an object.
	// Servers should convert recognized schemas to the latest internal value, and
	// may reject unrecognized values.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
	// +optional
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,2,opt,name=apiVersion"`
}

// ExecCredential is used by exec-based plugins to communicate credentials to
// HTTP transports.
type ExecCredential struct {
	TypeMeta `json:",inline"`

	// Status is filled in by the plugin and holds the credentials that the transport
	// should use to contact the API.
	// +optional
	Status *ExecCredentialStatus `json:"status,omitempty"`
}

// ExecCredentialStatus holds credentials for the transport to use.
//
// Token and ClientKeyData are sensitive fields. This data should only be
// transmitted in-memory between client and exec plugin process. Exec plugin
// itself should at least be protected via file permissions.
type ExecCredentialStatus struct {
	// ExpirationTimestamp indicates a time when the provided credentials expire.
	// +optional
	ExpirationTimestamp time.Time `json:"expirationTimestamp,omitempty"`
	// Token is a bearer token used by the client for request authentication.
	Token string `json:"token,omitempty"`
	// PEM-encoded client TLS certificates (including intermediates, if any).
	ClientCertificateData string `json:"clientCertificateData,omitempty"`
	// PEM-encoded private key for the above certificate.
	ClientKeyData string `json:"clientKeyData,omitempty"`
}
